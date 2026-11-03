// Package registry holds the set of assets compiled into the platform.
package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"agentic-platform/agent-assets/sdk"
)

var idPattern = regexp.MustCompile(`^[a-z0-9-]+\.(llm|tool|mcp|guardrail)\.[a-z0-9-]+$`)

// Registry is the catalog of registered assets.
type Registry struct {
	assets map[string]sdk.Asset
	order  []string
	errs   []error
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
		if m.ConfigSchema == nil {
			errs = append(errs, fmt.Errorf("%s: config schema is required", m.ID))
		} else if _, err := json.Marshal(m.ConfigSchema); err != nil {
			errs = append(errs, fmt.Errorf("%s: config schema does not serialize: %w", m.ID, err))
		}
	}
	return errors.Join(errs...)
}

// Manifests returns all manifests sorted by id, optionally filtered by kind.
func (r *Registry) Manifests(kind sdk.Kind) []sdk.Manifest {
	out := make([]sdk.Manifest, 0, len(r.assets))
	for _, a := range r.assets {
		if m := a.Manifest(); kind == "" || m.Kind == kind {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
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
