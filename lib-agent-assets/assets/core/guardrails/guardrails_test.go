package guardrails

import (
	"context"
	"encoding/json"
	"iter"
	"testing"

	"google.golang.org/adk/model"
	"google.golang.org/genai"

	"agentic-platform/lib-agent-assets/pkg/assets"
)

func TestPIIRedact(t *testing.T) {
	tests := []struct {
		name, cfg, in, want string
		action              assets.Action
	}{
		{"email", ``, "mail me at ana.silva@example.com please", "mail me at [EMAIL] please", assets.ActionRedact},
		{"cpf", ``, "my cpf is 123.456.789-09", "my cpf is [CPF]", assets.ActionRedact},
		{"card with luhn", ``, "card 4111 1111 1111 1111 ok", "card [CREDIT_CARD] ok", assets.ActionRedact},
		{"phone", ``, "call +351 912 345 678", "call [PHONE]", assets.ActionRedact},
		{"nothing", ``, "what's the weather in Lisbon?", "", assets.ActionAllow},
		{"entity disabled", `{"entities":["cpf"]}`, "a@b.com", "", assets.ActionAllow},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			check, err := PIIRedact{}.New(context.Background(), json.RawMessage(tt.cfg), assets.Deps{})
			if err != nil {
				t.Fatal(err)
			}
			res, err := check(context.Background(), assets.GuardInput{Stage: assets.StageInput, Text: tt.in})
			if err != nil {
				t.Fatal(err)
			}
			if res.Action != tt.action || res.Redacted != tt.want {
				t.Fatalf("got %s %q, want %s %q", res.Action, res.Redacted, tt.action, tt.want)
			}
		})
	}
}

func TestPIIRedactRejectsUnknownEntity(t *testing.T) {
	if _, err := (PIIRedact{}).New(context.Background(), json.RawMessage(`{"entities":["ssn"]}`), assets.Deps{}); err == nil {
		t.Fatal("expected error for unknown entity")
	}
}

type fakeLLM struct{ reply string }

func (f fakeLLM) Name() string { return "fake" }
func (f fakeLLM) GenerateContent(context.Context, *model.LLMRequest, bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		yield(&model.LLMResponse{Content: genai.NewContentFromText(f.reply, genai.RoleModel)}, nil)
	}
}

func TestPromptInjection(t *testing.T) {
	tests := []struct {
		reply  string
		cfg    string
		action assets.Action
	}{
		{`{"injection": true, "reason": "asks to ignore instructions"}`, ``, assets.ActionBlock},
		{"Sure: ```json\n{\"injection\": false, \"reason\": \"normal question\"}\n```", ``, assets.ActionAllow},
		{`not json`, ``, assets.ActionBlock},
		{`not json`, `{"failMode":"open"}`, assets.ActionAllow},
	}
	for _, tt := range tests {
		deps := assets.Deps{LLM: func(context.Context, string) (model.LLM, error) { return fakeLLM{tt.reply}, nil }}
		check, err := PromptInjection{}.New(context.Background(), json.RawMessage(tt.cfg), deps)
		if err != nil {
			t.Fatal(err)
		}
		res, err := check(context.Background(), assets.GuardInput{Stage: assets.StageInput, Text: "hi"})
		if err != nil {
			t.Fatal(err)
		}
		if res.Action != tt.action {
			t.Errorf("reply %q cfg %q: got %s, want %s", tt.reply, tt.cfg, res.Action, tt.action)
		}
	}
}
