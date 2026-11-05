// Package llmconfig loads and validates the gateway's upstream providers (llms.yaml).
package llmconfig

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"agentic-platform/ms-llm-gateway/config"
)

// ProviderType is the wire protocol an upstream speaks.
type ProviderType string

const (
	TypeAnthropic ProviderType = "anthropic"
	// TypeOpenAI is the OpenAI Chat Completions protocol (/v1/chat/completions),
	// spoken by OpenAI and most LLM gateways.
	TypeOpenAI ProviderType = "openai"
)

// DefaultRefresh is how often discovered model lists are refreshed.
const DefaultRefresh = 5 * time.Minute

// Config is the whole llms.yaml file.
type Config struct {
	Defaults  Defaults            `yaml:"defaults"`
	Providers map[string]Provider `yaml:"providers"`
}

type Defaults struct {
	// Model is the gateway's default model, as "<provider>/<model>".
	Model string `yaml:"model"`
}

// Provider is one upstream endpoint and its models.
type Provider struct {
	Key         string       `yaml:"-"` // map key, set by Parse
	Name        string       `yaml:"name"`
	Description string       `yaml:"description"`
	Type        ProviderType `yaml:"type"`
	BaseURL     string       `yaml:"baseURL"`
	// Auth is "api-key" (default) or "none".
	Auth string `yaml:"auth"`
	// APIKeyEnv names the environment variable holding the API key.
	APIKeyEnv string `yaml:"apiKeyEnv"`
	// Headers are sent on every request. Values may use ${VAR} env references.
	Headers map[string]string `yaml:"headers"`
	// StreamUsage forwards stream_options.include_usage to OpenAI-compatible
	// upstreams. Turn off for upstreams that reject it.
	StreamUsage *bool     `yaml:"streamUsage"`
	Discover    *Discover `yaml:"discover"`
	Models      []Model   `yaml:"models"`
	Params      Params    `yaml:"params"`
}

// Model is a model offered by a provider.
type Model struct {
	ID      string `yaml:"id"`
	Label   string `yaml:"label"`
	Default bool   `yaml:"default"`
	// Effort marks models that accept a reasoning effort (Anthropic
	// output_config.effort, OpenAI reasoning_effort). It is dropped for others.
	Effort bool `yaml:"effort"`
}

// Discover lists models from GET {baseURL}/models (OpenAI-compatible providers).
type Discover struct {
	Include []string      `yaml:"include"`
	Exclude []string      `yaml:"exclude"`
	Refresh time.Duration `yaml:"refresh"`
}

// Params are defaults for requests that don't set them.
type Params struct {
	MaxTokens int64  `yaml:"maxTokens"`
	Effort    string `yaml:"effort"` // low | medium | high, for models flagged effort
}

// ModelID is the id clients use on the gateway: "<provider>/<model>".
func ModelID(provider, model string) string { return provider + "/" + model }

// NeedsKey reports whether the provider authenticates with an API key.
func (p Provider) NeedsKey() bool { return p.Auth != "none" }

// APIKey reads the key from the environment ("" when auth is none or unset).
func (p Provider) APIKey() string {
	if !p.NeedsKey() {
		return ""
	}
	return os.Getenv(p.APIKeyEnv)
}

// CredentialsConfigured reports whether the key env var is set (or none is needed).
func (p Provider) CredentialsConfigured() bool { return !p.NeedsKey() || p.APIKey() != "" }

// StreamUsageEnabled defaults to true.
func (p Provider) StreamUsageEnabled() bool { return p.StreamUsage == nil || *p.StreamUsage }

// ExpandedHeaders resolves ${VAR} references. Call it per request, so resolved
// values never end up in logs or health output.
func (p Provider) ExpandedHeaders() map[string]string {
	out := make(map[string]string, len(p.Headers))
	for k, v := range p.Headers {
		out[k] = os.ExpandEnv(v)
	}
	return out
}

// RefreshInterval is how often discovery runs again.
func (p Provider) RefreshInterval() time.Duration {
	if p.Discover == nil || p.Discover.Refresh <= 0 {
		return DefaultRefresh
	}
	return p.Discover.Refresh
}

// Model returns the configured model with the given id.
func (p Provider) Model(id string) (Model, bool) {
	i := slices.IndexFunc(p.Models, func(m Model) bool { return m.ID == id })
	if i < 0 {
		return Model{}, false
	}
	return p.Models[i], true
}

// Sorted returns providers ordered by key.
func (c Config) Sorted() []Provider {
	keys := make([]string, 0, len(c.Providers))
	for k := range c.Providers {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	out := make([]Provider, 0, len(keys))
	for _, k := range keys {
		out = append(out, c.Providers[k])
	}
	return out
}

// Default returns the embedded llms.yaml.
func Default() Config {
	c, err := Parse(config.LLMs)
	if err != nil {
		panic(fmt.Sprintf("embedded llms.yaml is invalid: %v", err))
	}
	return c
}

// Load reads path, or the embedded default when path is empty.
func Load(path string) (Config, error) {
	if path == "" {
		return Parse(config.LLMs)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read LLM config: %w", err)
	}
	c, err := Parse(b)
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}

// Parse decodes strictly (unknown fields are errors), applies defaults and validates.
func Parse(b []byte) (Config, error) {
	var c Config
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return c, fmt.Errorf("invalid LLM config: %w", err)
	}
	for key, p := range c.Providers {
		p.Key = key
		if p.Name == "" {
			p.Name = key
		}
		if p.Description == "" {
			protocol := map[ProviderType]string{TypeAnthropic: "Anthropic", TypeOpenAI: "OpenAI-compatible"}[p.Type]
			p.Description = fmt.Sprintf("Models served by %s (%s API).", p.Name, protocol)
		}
		if p.Params.MaxTokens == 0 {
			p.Params.MaxTokens = 4096
		}
		c.Providers[key] = p
	}
	return c, c.Validate()
}

var (
	slugPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	envVarPattern = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
	effortLevels  = []string{"", "low", "medium", "high"}
)

// Validate checks the rules described in llms.yaml.
func (c Config) Validate() error {
	var errs []error
	add := func(key, format string, args ...any) {
		errs = append(errs, fmt.Errorf("providers.%s: %s", key, fmt.Sprintf(format, args...)))
	}
	if len(c.Providers) == 0 {
		errs = append(errs, errors.New("at least one provider is required"))
	}
	for _, p := range c.Sorted() {
		k := p.Key
		if !slugPattern.MatchString(k) {
			add(k, "key must be lowercase letters, digits and '-'")
		}
		switch p.Type {
		case TypeAnthropic, TypeOpenAI:
		default:
			add(k, "type must be %q or %q, got %q", TypeAnthropic, TypeOpenAI, p.Type)
		}
		switch p.Auth {
		case "", "api-key":
			if p.APIKeyEnv == "" {
				add(k, "apiKeyEnv is required (the name of the env var holding the key), or set auth: none")
			} else if !envVarPattern.MatchString(p.APIKeyEnv) {
				add(k, "apiKeyEnv must be an environment variable name like MY_API_KEY, not a key")
			}
		case "none":
		default:
			add(k, "auth must be \"api-key\" or \"none\"")
		}
		if p.BaseURL != "" {
			if u, err := url.Parse(p.BaseURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				add(k, "baseURL %q must be an http(s) URL", p.BaseURL)
			}
		} else if p.Type == TypeOpenAI {
			add(k, "baseURL is required for openai providers (for example https://llm.internal/v1)")
		}
		if p.Discover != nil && p.Type != TypeOpenAI {
			add(k, "discover is only supported for openai providers")
		}
		if len(p.Models) == 0 && p.Discover == nil {
			add(k, "list at least one model, or configure discover")
		}
		seen := map[string]bool{}
		defaults := 0
		for _, m := range p.Models {
			if m.ID == "" {
				add(k, "every model needs an id")
			}
			if seen[m.ID] {
				add(k, "duplicate model %q", m.ID)
			}
			seen[m.ID] = true
			if m.Default {
				defaults++
			}
		}
		if defaults > 1 {
			add(k, "only one model can be the default")
		}
		if p.Params.MaxTokens < 1 {
			add(k, "params.maxTokens must be positive")
		}
		if !slices.Contains(effortLevels, p.Params.Effort) {
			add(k, "params.effort must be low, medium or high")
		}
	}
	if id := c.Defaults.Model; id != "" {
		prov, model, ok := strings.Cut(id, "/")
		p, known := c.Providers[prov]
		switch {
		case !ok || !known:
			errs = append(errs, fmt.Errorf("defaults.model: %q must be \"<provider>/<model>\" with a configured provider", id))
		case p.Discover == nil:
			if _, ok := p.Model(model); !ok {
				errs = append(errs, fmt.Errorf("defaults.model: provider %q has no model %q", prov, model))
			}
		}
	}
	return errors.Join(errs...)
}

// Glob compiles a model filter: "*" matches any run of characters (including
// "/", which appears in gateway model ids) and "?" any single character.
func Glob(pattern string) (*regexp.Regexp, error) {
	re := regexp.QuoteMeta(pattern)
	re = strings.ReplaceAll(re, `\*`, ".*")
	re = strings.ReplaceAll(re, `\?`, ".")
	return regexp.Compile("^" + re + "$")
}

// MatchAny reports whether id matches any of the globs.
func MatchAny(globs []string, id string) bool {
	for _, g := range globs {
		if re, err := Glob(g); err == nil && re.MatchString(id) {
			return true
		}
	}
	return false
}
