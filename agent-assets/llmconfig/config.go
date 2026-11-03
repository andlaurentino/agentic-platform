// Package llmconfig loads and validates the LLM provider configuration (llms.yaml).
package llmconfig

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"agentic-platform/agent-assets/config"
)

// ProviderType is the wire protocol a provider speaks.
type ProviderType string

const (
	TypeAnthropic ProviderType = "anthropic"
	// TypeOpenAI is the OpenAI Chat Completions protocol (/v1/chat/completions),
	// spoken by OpenAI and most LLM gateways.
	TypeOpenAI ProviderType = "openai"
)

// Config is the whole llms.yaml file.
type Config struct {
	Defaults  Defaults            `yaml:"defaults"`
	Providers map[string]Provider `yaml:"providers"`
}

type Defaults struct {
	// GuardrailModel is the model L2 guardrails use, as "<provider>:<model>".
	GuardrailModel string `yaml:"guardrailModel"`
}

// Provider is one endpoint and its models. It becomes one LLM asset.
type Provider struct {
	Key         string       `yaml:"-"` // map key, set by Parse
	Domain      string       `yaml:"domain"`
	Name        string       `yaml:"name"`
	Description string       `yaml:"description"`
	Owner       string       `yaml:"owner"`
	Lifecycle   string       `yaml:"lifecycle"`
	Tags        []string     `yaml:"tags"`
	Type        ProviderType `yaml:"type"`
	BaseURL     string       `yaml:"baseURL"`
	// Auth is "api-key" (default) or "none".
	Auth string `yaml:"auth"`
	// APIKeyEnv names the environment variable holding the API key.
	APIKeyEnv string `yaml:"apiKeyEnv"`
	// Headers are sent on every request. Values may use ${VAR} env references.
	Headers map[string]string `yaml:"headers"`
	// StreamUsage asks OpenAI-compatible endpoints for token usage while
	// streaming (stream_options.include_usage). Turn off for gateways that reject it.
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
	// Effort marks Anthropic models that accept output_config.effort.
	Effort bool `yaml:"effort"`
}

// Discover lists models from GET {baseURL}/models (OpenAI-compatible providers).
type Discover struct {
	Include []string `yaml:"include"`
	Exclude []string `yaml:"exclude"`
}

// Params are default request parameters; builders can override them per agent.
type Params struct {
	MaxTokens       int64  `yaml:"maxTokens"`
	Effort          string `yaml:"effort"`          // anthropic: low | medium | high
	ReasoningEffort string `yaml:"reasoningEffort"` // openai reasoning models: low | medium | high
}

// AssetID is the catalog id of the provider's LLM asset.
func (p Provider) AssetID() string { return p.Domain + ".llm." + p.Key }

// NeedsKey reports whether the provider authenticates with an API key.
func (p Provider) NeedsKey() bool { return p.Auth != "none" }

// StreamUsageEnabled defaults to true.
func (p Provider) StreamUsageEnabled() bool { return p.StreamUsage == nil || *p.StreamUsage }

// ExpandedHeaders resolves ${VAR} references. Call it when connecting, so
// resolved values never end up in manifests or logs.
func (p Provider) ExpandedHeaders() map[string]string {
	out := make(map[string]string, len(p.Headers))
	for k, v := range p.Headers {
		out[k] = os.ExpandEnv(v)
	}
	return out
}

// DefaultModel returns the model marked default, or the first one.
func (p Provider) DefaultModel() string {
	for _, m := range p.Models {
		if m.Default {
			return m.ID
		}
	}
	if len(p.Models) > 0 {
		return p.Models[0].ID
	}
	return ""
}

// Model returns the configured model with the given id.
func (p Provider) Model(id string) (Model, bool) {
	for _, m := range p.Models {
		if m.ID == id {
			return m, true
		}
	}
	return Model{}, false
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

// SplitRef splits "<provider>:<model>". A ref without ":" has no provider.
func SplitRef(ref string) (provider, model string) {
	if p, m, ok := strings.Cut(ref, ":"); ok {
		return p, m
	}
	return "", ref
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
		if p.Domain == "" {
			p.Domain = "core"
		}
		if p.Owner == "" {
			p.Owner = "@ai-platform"
		}
		if p.Lifecycle == "" {
			p.Lifecycle = "stable"
		}
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
		if !slugPattern.MatchString(p.Domain) {
			add(k, "domain %q must be lowercase letters, digits and '-'", p.Domain)
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
			add(k, "baseURL is required for openai providers (for example https://llm-gateway.internal/v1)")
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
			if m.Effort && p.Type != TypeAnthropic {
				add(k, "model %q: effort is only supported for anthropic providers", m.ID)
			}
		}
		if defaults > 1 {
			add(k, "only one model can be the default")
		}
		if p.Discover != nil {
			for _, g := range append(slices.Clone(p.Discover.Include), p.Discover.Exclude...) {
				if _, err := path.Match(g, ""); err != nil {
					add(k, "invalid glob %q", g)
				}
			}
		}
		if p.Params.MaxTokens < 1 {
			add(k, "params.maxTokens must be positive")
		}
		if !slices.Contains(effortLevels, p.Params.Effort) || !slices.Contains(effortLevels, p.Params.ReasoningEffort) {
			add(k, "params effort values must be low, medium or high")
		}
	}
	if ref := c.Defaults.GuardrailModel; ref != "" {
		prov, model := SplitRef(ref)
		p, ok := c.Providers[prov]
		switch {
		case !ok:
			errs = append(errs, fmt.Errorf("defaults.guardrailModel: provider %q is not configured (use \"<provider>:<model>\")", prov))
		case p.Discover == nil:
			if _, ok := p.Model(model); !ok {
				errs = append(errs, fmt.Errorf("defaults.guardrailModel: provider %q has no model %q", prov, model))
			}
		}
	}
	return errors.Join(errs...)
}
