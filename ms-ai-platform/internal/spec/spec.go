// Package spec defines the orchestration document builders edit and the rules it must follow.
package spec

import (
	"encoding/json"
	"strings"
)

// Orchestration is a graph of agents with one root, with the agents resolved
// from the library. Validation, compilation and runs work on this form.
type Orchestration struct {
	RootAgent  string              `json:"rootAgent"`
	Agents     []Agent             `json:"agents"`
	Edges      []Edge              `json:"edges"`
	Guardrails []AssetRef          `json:"guardrails"` // applied to every agent
	Layout     map[string]Position `json:"layout"`
}

// Graph is how an orchestration is stored: the ids of the library agents it
// uses and how they connect. Agents are shared, so editing one changes every
// orchestration that uses it.
type Graph struct {
	RootAgent  string              `json:"rootAgent"`
	Agents     []string            `json:"agents"`
	Edges      []Edge              `json:"edges"`
	Guardrails []AssetRef          `json:"guardrails"` // applied to every agent
	Layout     map[string]Position `json:"layout"`
}

// Agent is a reusable agent from the library.
type Agent struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Description  string     `json:"description"`
	Instructions string     `json:"instructions"`
	Model        AssetRef   `json:"model"`
	Skills       []string   `json:"skills"`
	Tools        []ToolRef  `json:"tools"`
	Guardrails   []AssetRef `json:"guardrails"`
}

// AssetRef points at a catalog asset plus the builder's config for it.
type AssetRef struct {
	Asset  string          `json:"asset"`
	Config json.RawMessage `json:"config,omitempty"`
}

// ToolRef is either a native tool asset or a selection of tools from an MCP asset.
type ToolRef struct {
	Asset  string          `json:"asset,omitempty"`
	Config json.RawMessage `json:"config,omitempty"`
	MCP    string          `json:"mcp,omitempty"`
	Tools  []string        `json:"tools,omitempty"`
}

// EdgeMode is how a parent agent delegates to a child.
type EdgeMode string

const (
	// EdgeTransfer hands the conversation to the child (ADK sub-agent).
	EdgeTransfer EdgeMode = "transfer"
	// EdgeTool calls the child like a tool and gets its answer back (ADK AgentTool).
	EdgeTool EdgeMode = "tool"
)

type Edge struct {
	From string   `json:"from"`
	To   string   `json:"to"`
	Mode EdgeMode `json:"mode"`
}

type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Normalize replaces nil slices and maps so the JSON stays stable for clients.
func (o *Orchestration) Normalize() {
	if o.Agents == nil {
		o.Agents = []Agent{}
	}
	if o.Edges == nil {
		o.Edges = []Edge{}
	}
	if o.Guardrails == nil {
		o.Guardrails = []AssetRef{}
	}
	if o.Layout == nil {
		o.Layout = map[string]Position{}
	}
	for i := range o.Agents {
		o.Agents[i].Normalize()
	}
	for i := range o.Edges {
		if o.Edges[i].Mode == "" {
			o.Edges[i].Mode = EdgeTransfer
		}
	}
}

// Normalize replaces nil slices and maps, defaults edge modes, and drops edges
// and positions of agents the graph does not use.
func (g *Graph) Normalize() {
	if g.Agents == nil {
		g.Agents = []string{}
	}
	if g.Guardrails == nil {
		g.Guardrails = []AssetRef{}
	}
	used := map[string]bool{}
	for _, id := range g.Agents {
		used[id] = true
	}
	edges := []Edge{}
	for _, e := range g.Edges {
		if used[e.From] && used[e.To] {
			if e.Mode == "" {
				e.Mode = EdgeTransfer
			}
			edges = append(edges, e)
		}
	}
	g.Edges = edges
	layout := map[string]Position{}
	for id, p := range g.Layout {
		if used[id] {
			layout[id] = p
		}
	}
	g.Layout = layout
}

// Resolve returns the orchestration with its agents taken from the library.
// Agents missing from the library are left out, so validation reports the
// edges that still point at them.
func (g Graph) Resolve(library map[string]Agent) Orchestration {
	o := Orchestration{RootAgent: g.RootAgent, Edges: g.Edges, Guardrails: g.Guardrails, Layout: g.Layout}
	for _, id := range g.Agents {
		if a, ok := library[id]; ok {
			a.ID = id
			o.Agents = append(o.Agents, a)
		}
	}
	o.Normalize()
	return o
}

// Graph returns the stored form of the orchestration.
func (o Orchestration) Graph() Graph {
	g := Graph{RootAgent: o.RootAgent, Edges: o.Edges, Guardrails: o.Guardrails, Layout: o.Layout}
	for _, a := range o.Agents {
		g.Agents = append(g.Agents, a.ID)
	}
	g.Normalize()
	return g
}

// Normalize replaces nil slices so the JSON stays stable for clients.
func (a *Agent) Normalize() {
	if a.Skills == nil {
		a.Skills = []string{}
	}
	if a.Tools == nil {
		a.Tools = []ToolRef{}
	}
	if a.Guardrails == nil {
		a.Guardrails = []AssetRef{}
	}
}

// Agent returns the agent with the given id.
func (o *Orchestration) Agent(id string) (*Agent, bool) {
	for i := range o.Agents {
		if o.Agents[i].ID == id {
			return &o.Agents[i], true
		}
	}
	return nil, false
}

// Children returns the ids of agents reachable from id through edges of the given mode.
func (o *Orchestration) Children(id string, mode EdgeMode) []string {
	var out []string
	for _, e := range o.Edges {
		if e.From == id && e.Mode == mode {
			out = append(out, e.To)
		}
	}
	return out
}

// Reachable returns agent ids reachable from the root through any edge, root included.
func (o *Orchestration) Reachable() map[string]bool {
	seen := map[string]bool{}
	var walk func(string)
	walk = func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		for _, e := range o.Edges {
			if e.From == id {
				walk(e.To)
			}
		}
	}
	if o.RootAgent != "" {
		walk(o.RootAgent)
	}
	return seen
}

// RuntimeName is the agent name the model sees (used for transfers). It is a
// readable slug of the display name.
func RuntimeName(a Agent) string {
	var sb strings.Builder
	lastUnderscore := false
	for _, r := range strings.ToLower(strings.TrimSpace(a.Name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			sb.WriteRune(r)
			lastUnderscore = false
		case !lastUnderscore && sb.Len() > 0:
			sb.WriteByte('_')
			lastUnderscore = true
		}
	}
	name := strings.TrimSuffix(sb.String(), "_")
	if name == "" || (name[0] >= '0' && name[0] <= '9') {
		name = "agent_" + name
	}
	return name
}

// NewStarter returns a new orchestration's graph and its one root agent, which
// is added to the library.
func NewStarter(agentID string) (Graph, Agent) {
	a := Agent{
		ID:           agentID,
		Name:         "Assistant",
		Description:  "Main entry point. Answers questions and routes to specialists.",
		Instructions: "You are a helpful assistant. Answer briefly and clearly.",
		Model:        AssetRef{Asset: DefaultLLM, Config: json.RawMessage(`{}`)},
	}
	a.Normalize()
	g := Graph{RootAgent: agentID, Agents: []string{agentID}, Layout: map[string]Position{agentID: {X: 0, Y: 0}}}
	g.Normalize()
	return g, a
}
