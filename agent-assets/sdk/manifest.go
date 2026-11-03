// Package sdk defines the contracts every asset implements. ms-ai-platform
// imports this package to load and run assets.
package sdk

import (
	"encoding/json"
	"fmt"

	"github.com/invopop/jsonschema"
)

// Kind is the type of an asset.
type Kind string

const (
	KindLLM       Kind = "llm"
	KindTool      Kind = "tool"
	KindMCP       Kind = "mcp"
	KindGuardrail Kind = "guardrail"
)

// Lifecycle tells builders how stable an asset is.
type Lifecycle string

const (
	LifecycleExperimental Lifecycle = "experimental"
	LifecycleStable       Lifecycle = "stable"
	LifecycleDeprecated   Lifecycle = "deprecated"
)

// SecretRef names a secret the asset needs. Values are never part of a manifest.
type SecretRef struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// Manifest describes an asset to the platform and the UI.
type Manifest struct {
	ID           string             `json:"id"` // "{domain}.{kind}.{name}"
	Kind         Kind               `json:"kind"`
	Domain       string             `json:"domain"`
	Name         string             `json:"name"`
	Description  string             `json:"description"`
	Owner        string             `json:"owner"`
	Tags         []string           `json:"tags,omitempty"`
	Lifecycle    Lifecycle          `json:"lifecycle"`
	ConfigSchema *jsonschema.Schema `json:"configSchema"`
	Secrets      []SecretRef        `json:"secrets,omitempty"`
	// Extra holds kind-specific, UI-facing metadata (e.g. guardrail level, MCP allow-list).
	Extra map[string]any `json:"extra,omitempty"`
}

// Secrets resolves secret values by name.
type Secrets interface {
	Get(name string) (string, error)
}

// SchemaFor generates the JSON Schema for a config struct. Use `jsonschema`
// struct tags for enums, defaults and descriptions.
func SchemaFor[T any]() *jsonschema.Schema {
	r := &jsonschema.Reflector{DoNotReference: true, ExpandedStruct: true}
	s := r.Reflect(new(T))
	s.Version = ""
	return s
}

// DecodeConfig unmarshals raw config on top of defaults.
func DecodeConfig[T any](raw json.RawMessage, defaults T) (T, error) {
	cfg := defaults
	if len(raw) == 0 || string(raw) == "null" {
		return cfg, nil
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return cfg, fmt.Errorf("invalid config: %w", err)
	}
	return cfg, nil
}
