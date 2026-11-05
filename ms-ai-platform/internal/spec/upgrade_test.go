package spec

import (
	"encoding/json"
	"testing"
)

func TestUpgradeLegacyLLMs(t *testing.T) {
	registered := func(id string) bool { return id == "core.llm.gateway" || id == "core.llm.company" }
	o := Orchestration{
		Agents: []Agent{
			{ID: "a", Model: AssetRef{Asset: "core.llm.claude-haiku", Config: json.RawMessage(`{"model":"claude-haiku-5-5","effort":"low"}`)},
				Guardrails: []AssetRef{{Asset: "core.guardrail.prompt-injection", Config: json.RawMessage(`{"model":"claude-haiku:claude-haiku-4-5"}`)}}},
			{ID: "b", Model: AssetRef{Asset: "core.llm.llm-gateway", Config: json.RawMessage(`{"reasoningEffort":"high"}`)}},
			{ID: "c", Model: AssetRef{Asset: "core.llm.company", Config: json.RawMessage(`{"model":"gpt-4.1"}`)}},
		},
		Guardrails: []AssetRef{
			{Asset: "core.guardrail.prompt-injection", Config: json.RawMessage(`{"model":"company:gpt-4.1"}`)},
			{Asset: "core.guardrail.pii-redact"},
		},
	}
	if !UpgradeLegacyLLMs(&o, registered) {
		t.Fatal("expected changes")
	}
	for i, want := range []string{
		`core.llm.gateway {"effort":"low","model":"claude-haiku/claude-haiku-5-5"}`,
		`core.llm.gateway {"effort":"high"}`,
		`core.llm.company {"model":"gpt-4.1"}`,
	} {
		if got := o.Agents[i].Model.Asset + " " + string(o.Agents[i].Model.Config); got != want {
			t.Errorf("agent %d: %s, want %s", i, got, want)
		}
	}
	if got := string(o.Agents[0].Guardrails[0].Config); got != `{"model":"gateway:claude-haiku/claude-haiku-4-5"}` {
		t.Errorf("agent guardrail: %s", got)
	}
	if got := string(o.Guardrails[0].Config); got != `{"model":"company:gpt-4.1"}` {
		t.Errorf("registered provider ref changed: %s", got)
	}
	if UpgradeLegacyLLMs(&o, registered) {
		t.Error("second upgrade should be a no-op")
	}
}
