// Package registry holds the set of assets compiled into the platform.
package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/invopop/jsonschema"

	"agentic-platform/agent-assets/sdk"
)

var idPattern = regexp.MustCompile(`^[a-z0-9-]+\.(llm|tool|mcp|guardrail)\.[a-z0-9-]+$`)

// Registry is the catalog of registered assets.
type Registry struct {
	assets map[string]sdk.Asset
	order  []string
	errs   []error
	// defaultModel is the model reference used when a guardrail leaves its model empty.
	defaultModel string
}

// ModelRef identifies one model of one provider, as "<provider>:<model>".
type ModelRef struct {
	Ref   string `json:"ref"`
	Label string `json:"label"`
}

func New() *Registry {
	return &Registry{assets: map[string]sdk.Asset{}}
}

func (r *Registry) add(a sdk.Asset, want sdk.Kind) {
	m := a.Manifest()
	if m.Kind != want {
		r.errs = append(r.errs, fmt.Errorf("%s: registered as %s but manifest kind is %q", m.ID, want, m.Kind))
	}
	if _, dup := r.assets[m.ID]; dup {
		r.errs = append(r.errs, fmt.Errorf("%s: duplicate asset id", m.ID))
		return
	}
	r.assets[m.ID] = a
	r.order = append(r.order, m.ID)
}

// MustLLM, MustTool, MustMCP and MustGuardrail register an asset. Problems are
// collected and reported by Validate, so a domain can register everything first.
func (r *Registry) MustLLM(a sdk.LLM)             { r.add(a, sdk.KindLLM) }
func (r *Registry) MustTool(a sdk.Tool)           { r.add(a, sdk.KindTool) }
func (r *Registry) MustMCP(a sdk.MCP)             { r.add(a, sdk.KindMCP) }
func (r *Registry) MustGuardrail(a sdk.Guardrail) { r.add(a, sdk.KindGuardrail) }

// Validate checks the whole catalog.
func (r *Registry) Validate() error {
	errs := append([]error(nil), r.errs...)
	providerKeys := map[string]string{}
	for _, id := range r.order {
		m := r.assets[id].Manifest()
		if !idPattern.MatchString(m.ID) {
			errs = append(errs, fmt.Errorf("%s: id must look like {domain}.{kind}.{name}", m.ID))
		} else if parts := strings.Split(m.ID, "."); parts[0] != m.Domain || parts[1] != string(m.Kind) {
			errs = append(errs, fmt.Errorf("%s: id does not match domain %q / kind %q", m.ID, m.Domain, m.Kind))
		}
		if m.Name == "" || m.Description == "" {
			errs = append(errs, fmt.Errorf("%s: name and description are required", m.ID))
		}
		if !strings.HasPrefix(m.Owner, "@") {
			errs = append(errs, fmt.Errorf("%s: owner must be a team handle like @team", m.ID))
		}
		switch m.Lifecycle {
		case sdk.LifecycleExperimental, sdk.LifecycleStable, sdk.LifecycleDeprecated:
		default:
			errs = append(errs, fmt.Errorf("%s: invalid lifecycle %q", m.ID, m.Lifecycle))
		}
		if mp, ok := r.assets[id].(sdk.ModelProvider); ok {
			if other, dup := providerKeys[mp.ProviderKey()]; dup {
				errs = append(errs, fmt.Errorf("%s: provider key %q is also used by %s", m.ID, mp.ProviderKey(), other))
			}
			providerKeys[mp.ProviderKey()] = m.ID
		}
		if m.ConfigSchema == nil {
			errs = append(errs, fmt.Errorf("%s: config schema is required", m.ID))
		} else if _, err := json.Marshal(m.ConfigSchema); err != nil {
			errs = append(errs, fmt.Errorf("%s: config schema does not serialize: %w", m.ID, err))
		}
	}
	if r.defaultModel != "" {
		if _, _, err := r.ResolveModel(r.defaultModel); err != nil {
			errs = append(errs, fmt.Errorf("default guardrail model: %w", err))
		}
	}
	return errors.Join(errs...)
}

// Manifests returns all manifests sorted by id, optionally filtered by kind.
func (r *Registry) Manifests(kind sdk.Kind) []sdk.Manifest {
	out := make([]sdk.Manifest, 0, len(r.assets))
	for _, a := range r.assets {
		if m := r.enrich(a.Manifest()); kind == "" || m.Kind == kind {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Manifest returns one asset's manifest, with model pickers filled in.
func (r *Registry) Manifest(id string) (sdk.Manifest, bool) {
	a, ok := r.assets[id]
	if !ok {
		return sdk.Manifest{}, false
	}
	return r.enrich(a.Manifest()), true
}

// enrich fills config properties marked `x-llm-model` with every configured
// model, so assets like L2 guardrails can let builders pick a model.
func (r *Registry) enrich(m sdk.Manifest) sdk.Manifest {
	if m.ConfigSchema == nil || m.ConfigSchema.Properties == nil {
		return m
	}
	for pair := m.ConfigSchema.Properties.Oldest(); pair != nil; pair = pair.Next() {
		prop := pair.Value
		if v, ok := prop.Extras["x-llm-model"]; !ok || (v != true && v != "true") {
			continue
		}
		prop.OneOf = nil
		for _, ref := range r.ModelRefs() {
			prop.OneOf = append(prop.OneOf, &jsonschema.Schema{Const: ref.Ref, Title: ref.Label})
		}
		if r.defaultModel != "" {
			prop.Description = strings.TrimSpace(prop.Description + " Leave empty to use the platform default (" + r.defaultModel + ").")
		}
	}
	return m
}

// SetDefaultModel sets the model reference used when a guardrail leaves its model empty.
func (r *Registry) SetDefaultModel(ref string) { r.defaultModel = ref }

// DefaultModel returns the platform default model reference.
func (r *Registry) DefaultModel() string { return r.defaultModel }

// ModelRefs lists every model of every provider, sorted by provider then model.
func (r *Registry) ModelRefs() []ModelRef {
	var out []ModelRef
	for _, id := range r.sortedIDs() {
		mp, ok := r.assets[id].(sdk.ModelProvider)
		if !ok {
			continue
		}
		name := mp.Manifest().Name
		for _, m := range mp.Models() {
			label := m.Label
			if label == "" {
				label = m.ID
			}
			out = append(out, ModelRef{Ref: mp.ProviderKey() + ":" + m.ID, Label: name + " · " + label})
		}
	}
	return out
}

// ResolveModel finds the provider and model for "<provider>:<model>". A bare
// model id resolves to the first provider (by asset id) that offers it.
func (r *Registry) ResolveModel(ref string) (sdk.ModelProvider, string, error) {
	provider, modelID, hasProvider := strings.Cut(ref, ":")
	if !hasProvider {
		provider, modelID = "", ref
	}
	for _, id := range r.sortedIDs() {
		mp, ok := r.assets[id].(sdk.ModelProvider)
		if !ok || (provider != "" && mp.ProviderKey() != provider) {
			continue
		}
		if slices.ContainsFunc(mp.Models(), func(m sdk.ModelInfo) bool { return m.ID == modelID }) {
			return mp, modelID, nil
		}
		if provider != "" {
			return nil, "", fmt.Errorf("provider %q has no model %q", provider, modelID)
		}
	}
	if provider != "" {
		return nil, "", fmt.Errorf("LLM provider %q is not configured", provider)
	}
	return nil, "", fmt.Errorf("no LLM provider offers model %q", modelID)
}

func (r *Registry) sortedIDs() []string {
	ids := slices.Clone(r.order)
	slices.Sort(ids)
	return ids
}

func (r *Registry) Get(id string) (sdk.Asset, bool) {
	a, ok := r.assets[id]
	return a, ok
}

func (r *Registry) LLM(id string) (sdk.LLM, bool) {
	a, ok := r.assets[id].(sdk.LLM)
	return a, ok
}

func (r *Registry) Tool(id string) (sdk.Tool, bool) {
	a, ok := r.assets[id].(sdk.Tool)
	return a, ok
}

func (r *Registry) MCP(id string) (sdk.MCP, bool) {
	a, ok := r.assets[id].(sdk.MCP)
	return a, ok
}

func (r *Registry) Guardrail(id string) (sdk.Guardrail, bool) {
	a, ok := r.assets[id].(sdk.Guardrail)
	return a, ok
}
