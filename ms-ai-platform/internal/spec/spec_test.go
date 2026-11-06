package spec

import (
	"reflect"
	"testing"
)

func TestGraphResolveAndBack(t *testing.T) {
	g := Graph{
		RootAgent: "a",
		Agents:    []string{"a", "b", "gone"},
		Edges:     []Edge{{From: "a", To: "b"}, {From: "a", To: "gone", Mode: EdgeTool}, {From: "a", To: "stranger"}},
		Layout:    map[string]Position{"a": {X: 1}, "stranger": {X: 9}},
	}
	g.Normalize()
	// Edges and positions of agents the graph does not list are dropped; a missing mode means hand-off.
	if len(g.Edges) != 2 || g.Edges[0].Mode != EdgeTransfer || len(g.Layout) != 1 || g.Guardrails == nil {
		t.Fatalf("normalized graph = %+v", g)
	}

	library := map[string]Agent{"a": {Name: "Router"}, "b": {ID: "stale", Name: "Weather"}}
	o := g.Resolve(library)
	if len(o.Agents) != 2 || o.Agents[0].ID != "a" || o.Agents[1].ID != "b" || o.Agents[1].Skills == nil {
		t.Fatalf("resolved agents = %+v", o.Agents)
	}
	// The edge to the agent missing from the library is kept, so validation reports it.
	if len(o.Edges) != 2 {
		t.Fatalf("resolved edges = %+v", o.Edges)
	}

	back := o.Graph()
	if !reflect.DeepEqual(back.Agents, []string{"a", "b"}) || len(back.Edges) != 1 || back.RootAgent != "a" {
		t.Fatalf("graph = %+v", back)
	}
}

func TestNewStarter(t *testing.T) {
	g, a := NewStarter("agt_1")
	if g.RootAgent != "agt_1" || !reflect.DeepEqual(g.Agents, []string{"agt_1"}) || a.ID != "agt_1" || a.Model.Asset != DefaultLLM {
		t.Fatalf("starter = %+v %+v", g, a)
	}
}
