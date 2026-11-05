// Package llmconfig loads the LLM providers the platform connects to
// (llm-providers.yaml). Every provider is an OpenAI-compatible endpoint.
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

	"go.yaml.in/yaml/v3"

	"agentic-platform/ms-ai-platform/config"
)

// Config is the whole llm-providers.yaml file.
type Config struct {
	Defaults  Defaults            `yaml:"defaults"`
	Providers map[string]Provider `yaml:"providers"`
}

type Defaults struct {
	// GuardrailModel is the model L2 guardrails use, as "<provider>:<model>".
	GuardrailModel string `yaml:"guardrailModel"`
}

// Provider is one OpenAI-compatible endpoint. It becomes one LLM asset.
type Provider struct {
	Key         string `yaml:"-"` // map key, set by Parse
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	// BaseURL is expanded from the environment when the file is loaded.
	BaseURL string `yaml:"baseURL"`
	// Auth is "api-key" (default: the key is required), "optional" (sent when
	// the env var is set) or "none".
	Auth string `yaml:"auth"`
	// APIKeyEnv names the environment variable holding the API key.
	APIKeyEnv string `yaml:"apiKeyEnv"`
	// Headers are sent on every request. Values may use ${VAR} env references.
	Headers map[string]string `yaml:"headers"`
	// StreamUsage asks for token usage while streaming (stream_options.include_usage).
	StreamUsage *bool `yaml:"streamUsage"`
	// Include and Exclude filter the endpoint's GET /models with globs.
	Include []string `yaml:"include"`
	Exclude []string `yaml:"exclude"`
	// Models add labels and the default; they are also the list while the
	// endpoint has not been reached.
	Models []Model `yaml:"models"`
	Params Params  `yaml:"params"`
}

type Model struct {
	ID      string `yaml:"id"`
	Label   string `yaml:"label"`
	Default bool   `yaml:"default"`
}

// Params are defaults builders can override per agent.
type Params struct {
	MaxTokens int64  `yaml:"maxTokens"`
	Effort    string `yaml:"effort"` // sent as reasoning_effort when set
}

// AssetID is the catalog id of the provider's LLM asset.
func (p Provider) AssetID() string { return "core.llm." + p.Key }

// KeyRequired reports whether calls fail without the API key.
func (p Provider) KeyRequired() bool { return p.Auth == "" || p.Auth == "api-key" }

// UsesKey reports whether an API key is sent at all.
func (p Provider) UsesKey() bool { return p.Auth != "none" && p.APIKeyEnv != "" }

// CredentialsConfigured is false only when a required key is missing.
func (p Provider) CredentialsConfigured() bool {
	return !p.KeyRequired() || os.Getenv(p.APIKeyEnv) != ""
}

// StreamUsageEnabled defaults to true.
func (p Provider) StreamUsageEnabled() bool { return p.StreamUsage == nil || *p.StreamUsage }

// ExpandedHeaders resolves env references. Call it when connecting, so
// resolved values never end up in manifests or logs.
func (p Provider) ExpandedHeaders() map[string]string {
	out := make(map[string]string, len(p.Headers))
	for k, v := range p.Headers {
		out[k] = Expand(v)
	}
	return out
}

// Expand replaces ${VAR} and ${VAR:-default} with environment values.
func Expand(s string) string {
	return os.Expand(s, func(name string) string {
		name, def, hasDef := strings.Cut(name, ":-")
		if v := os.Getenv(name); v != "" || !hasDef {
			return v
		}
		return def
	})
}

// SplitRef splits "<provider>:<model>" at the first ":" (model ids may contain ":").
func SplitRef(ref string) (provider, model string, ok bool) {
	return strings.Cut(ref, ":")
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

// Default parses the embedded llm-providers.yaml.
func Default() Config {
	c, err := Parse(config.LLMProviders)
	if err != nil {
		panic(fmt.Sprintf("embedded llm-providers.yaml is invalid: %v", err))
	}
	return c
}

// Load reads path, or the embedded default when path is empty.
func Load(path string) (Config, error) {
	if path == "" {
		return Parse(config.LLMProviders)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read LLM providers: %w", err)
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
		return c, fmt.Errorf("invalid LLM providers config: %w", err)
	}
	for key, p := range c.Providers {
		p.Key = key
		p.BaseURL = Expand(p.BaseURL)
		if p.Name == "" {
			p.Name = key
		}
		if p.Description == "" {
			p.Description = fmt.Sprintf("Models served by %s (OpenAI-compatible API).", p.Name)
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

// Validate checks the rules described in llm-providers.yaml.
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
		if u, err := url.Parse(p.BaseURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			add(k, "baseURL %q must be an http(s) URL such as http://localhost:8082/v1", p.BaseURL)
		}
		switch p.Auth {
		case "", "api-key", "optional":
			if p.APIKeyEnv == "" && p.KeyRequired() {
				add(k, "apiKeyEnv is required (the name of the env var holding the key), or set auth: none")
			}
			if p.APIKeyEnv != "" && !envVarPattern.MatchString(p.APIKeyEnv) {
				add(k, "apiKeyEnv must be an environment variable name like MY_API_KEY, not a key")
			}
		case "none":
		default:
			add(k, "auth must be \"api-key\", \"optional\" or \"none\"")
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
	if ref := c.Defaults.GuardrailModel; ref != "" {
		prov, _, ok := SplitRef(ref)
		if _, known := c.Providers[prov]; !ok || !known {
			errs = append(errs, fmt.Errorf("defaults.guardrailModel: %q must be \"<provider>:<model>\" with a configured provider", ref))
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
