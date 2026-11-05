package spec

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"agentic-platform/lib-agent-assets/pkg/registry"
	"agentic-platform/ms-ai-platform/internal/llmproviders/gatewaytest"
	"agentic-platform/ms-ai-platform/internal/llmproviders/llmtest"
)

// testCatalog has the default gateway with one model. Nothing listens on the
// URL, so the configured model list is used.
func testCatalog() *registry.Registry {
	return llmtest.Catalog(llmtest.Gateway("http://127.0.0.1:1/v1", "claude-haiku/claude-haiku-5-5"))
}

func valid() Orchestration {
	o := Orchestration{
		RootAgent: "a",
		Agents: []Agent{
			{ID: "a", Name: "Router", Description: "routes", Model: AssetRef{Asset: "core.llm.gateway"}},
			{ID: "b", Name: "Weather", Description: "weather", Model: AssetRef{Asset: "core.llm.gateway"},
				Tools: []ToolRef{{MCP: "core.mcp.demo", Tools: []string{"get_weather"}}, {Asset: "core.tool.current-time"}}},
			{ID: "c", Name: "Summarizer", Description: "summarizes", Model: AssetRef{Asset: "core.llm.gateway"}},
		},
		Edges:      []Edge{{From: "a", To: "b", Mode: EdgeTransfer}, {From: "b", To: "c", Mode: EdgeTool}},
		Guardrails: []AssetRef{{Asset: "core.guardrail.pii-redact", Config: json.RawMessage(`{"entities":["email"]}`)}},
	}
	o.Normalize()
	return o
}

func TestValidate(t *testing.T) {
	v := NewValidator(testCatalog())
	noSkills := func(string) bool { return false }

	if ps := v.Validate(ptr(valid()), noSkills); len(ps) != 0 {
		t.Fatalf("valid spec has problems: %+v", ps)
	}

	tests := []struct {
		name   string
		mutate func(*Orchestration)
		want   string
	}{
		{"no root", func(o *Orchestration) { o.RootAgent = "" }, "choose a root agent"},
		{"missing model", func(o *Orchestration) { o.Agents[0].Model = AssetRef{} }, "choose a llm"},
		{"wrong kind", func(o *Orchestration) { o.Agents[0].Model.Asset = "core.tool.current-time" }, "expected a llm"},
		{"unknown asset", func(o *Orchestration) { o.Agents[0].Model.Asset = "core.llm.nope" }, "not in the catalog"},
		{"unknown model", func(o *Orchestration) { o.Agents[0].Model.Config = json.RawMessage(`{"model":"gpt-4"}`) }, `model "gpt-4" is not offered by LLM Gateway`},
		{"bad config", func(o *Orchestration) { o.Agents[0].Model.Config = json.RawMessage(`{"maxTokens":0}`) }, "LLM Gateway config"},
		{"unknown config field", func(o *Orchestration) { o.Agents[0].Model.Config = json.RawMessage(`{"temperature":1}`) }, "LLM Gateway config"},
		{"tool outside allow-list", func(o *Orchestration) { o.Agents[1].Tools[0].Tools = []string{"delete_everything"} }, "not in the allow-list"},
		{"missing skill", func(o *Orchestration) { o.Agents[0].Skills = []string{"skl_x"} }, `skill "skl_x" does not exist`},
		{"cycle", func(o *Orchestration) { o.Edges = append(o.Edges, Edge{From: "c", To: "b", Mode: EdgeTool}) }, "cycle"},
		{"two parents", func(o *Orchestration) { o.Edges = append(o.Edges, Edge{From: "c", To: "b", Mode: EdgeTransfer}) }, "only one parent"},
		{"edge into root", func(o *Orchestration) { o.Edges = append(o.Edges, Edge{From: "b", To: "a", Mode: EdgeTool}) }, "root agent cannot have a parent"},
		{"duplicate names", func(o *Orchestration) { o.Agents[1].Name = "router!" }, "too similar"},
		{"bad guardrail config", func(o *Orchestration) { o.Guardrails[0].Config = json.RawMessage(`{"entities":["ssn"]}`) }, "PII redaction config"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := valid()
			tt.mutate(&o)
			ps := v.Validate(&o, noSkills)
			if !HasErrors(ps) {
				t.Fatalf("expected an error containing %q, got %+v", tt.want, ps)
			}
			var msgs []string
			for _, p := range ps {
				msgs = append(msgs, p.Message)
			}
			if all := strings.Join(msgs, " | "); !strings.Contains(all, tt.want) {
				t.Fatalf("want %q in %s", tt.want, all)
			}
		})
	}
}

func TestUnreachableAgentIsWarning(t *testing.T) {
	o := valid()
	o.Agents = append(o.Agents, Agent{ID: "d", Name: "Orphan", Description: "x", Model: AssetRef{Asset: "core.llm.gateway"}})
	ps := NewValidator(testCatalog()).Validate(&o, nil)
	if HasErrors(ps) || len(ps) != 1 || ps[0].Severity != SeverityWarning {
		t.Fatalf("got %+v", ps)
	}
}

func TestRuntimeName(t *testing.T) {
	for in, want := range map[string]string{"Refunds Specialist": "refunds_specialist", "  Router!! ": "router", "42": "agent_42", "Ação": "a_o"} {
		if got := RuntimeName(Agent{Name: in}); got != want {
			t.Errorf("RuntimeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func ptr[T any](v T) *T { return &v }

func TestProviderWithoutModels(t *testing.T) {
	// Nothing listens on the URL and no models are configured.
	v := NewValidator(llmtest.Catalog(llmtest.Gateway("http://127.0.0.1:1/v1")))
	ps := v.Validate(ptr(valid()), nil)
	if !HasErrors(ps) || !strings.Contains(ps[0].Message, "LLM Gateway is unavailable") {
		t.Fatalf("got %+v", ps)
	}
}

// The validator sees model lists that change after it was created.
func TestValidatorSeesRefreshedModels(t *testing.T) {
	gw := gatewaytest.New(gatewaytest.Models("claude-haiku/claude-haiku-5-5"))
	defer gw.Close()
	c := llmtest.Catalog(llmtest.Gateway(gw.URL + "/v1"))
	v := NewValidator(c)
	o := valid()
	o.Agents[0].Model.Config = json.RawMessage(`{"model":"gw/new-model"}`)
	if ps := v.Validate(&o, nil); !HasErrors(ps) {
		t.Fatal("unknown model accepted")
	}
	gw.SetModels(gatewaytest.Models("claude-haiku/claude-haiku-5-5", "gw/new-model"))
	llm, _ := c.LLM("core.llm.gateway")
	llm.(interface{ Refresh(context.Context) bool }).Refresh(context.Background())
	if ps := v.Validate(&o, nil); HasErrors(ps) {
		t.Fatalf("refreshed model rejected: %+v", ps)
	}
}
