// Package guardrails holds guardrail assets.
package guardrails

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/invopop/jsonschema"

	"agentic-platform/agent-assets/sdk"
)

const PIIRedactID = "core.guardrail.pii-redact"

type PIIConfig struct {
	Entities []string `json:"entities,omitempty" jsonschema:"title=Entities to redact,uniqueItems=true"`
}

// JSONSchemaExtend adds the enum of supported entities to the generated schema.
func (PIIConfig) JSONSchemaExtend(s *jsonschema.Schema) {
	if p, ok := s.Properties.Get("entities"); ok {
		p.Items = &jsonschema.Schema{Type: "string", Enum: []any{"email", "phone", "cpf", "credit_card"}}
		p.Default = []string{"email", "phone", "cpf", "credit_card"}
	}
}

var piiPatterns = map[string]*regexp.Regexp{
	"email":       regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`),
	"cpf":         regexp.MustCompile(`\b\d{3}\.?\d{3}\.?\d{3}-?\d{2}\b`),
	"credit_card": regexp.MustCompile(`\b(?:\d[ -]?){12,18}\d\b`),
	"phone":       regexp.MustCompile(`\+?\(?\d[\d ().-]{7,}\d\b`),
}

// Order matters: credit cards and CPFs before phone numbers, so long digit runs are labeled correctly.
var piiOrder = []string{"email", "credit_card", "cpf", "phone"}

// PIIRedact is an L1 guardrail that masks personal data with regular expressions.
type PIIRedact struct{}

func (PIIRedact) Manifest() sdk.Manifest {
	return sdk.Manifest{
		ID:           PIIRedactID,
		Kind:         sdk.KindGuardrail,
		Domain:       "core",
		Name:         "PII redaction",
		Description:  "Masks emails, phone numbers, CPFs and credit card numbers in user input and model output.",
		Owner:        "@ai-platform",
		Tags:         []string{"pii", "privacy"},
		Lifecycle:    sdk.LifecycleStable,
		ConfigSchema: sdk.SchemaFor[PIIConfig](),
		Extra:        map[string]any{"level": sdk.L1, "stages": []sdk.Stage{sdk.StageInput, sdk.StageOutput}},
	}
}

func (PIIRedact) Level() sdk.GuardrailLevel { return sdk.L1 }
func (PIIRedact) Stages() []sdk.Stage       { return []sdk.Stage{sdk.StageInput, sdk.StageOutput} }

func (PIIRedact) New(_ context.Context, raw json.RawMessage, _ sdk.Deps) (sdk.Check, error) {
	cfg, err := sdk.DecodeConfig(raw, PIIConfig{Entities: piiOrder})
	if err != nil {
		return nil, err
	}
	enabled := map[string]bool{}
	for _, e := range cfg.Entities {
		if _, ok := piiPatterns[e]; !ok {
			return nil, fmt.Errorf("unknown entity %q", e)
		}
		enabled[e] = true
	}
	return func(_ context.Context, in sdk.GuardInput) (sdk.GuardResult, error) {
		text := in.Text
		var labels []string
		for _, name := range piiOrder {
			if !enabled[name] {
				continue
			}
			found := false
			text = piiPatterns[name].ReplaceAllStringFunc(text, func(m string) string {
				if name == "credit_card" && !luhn(m) {
					return m
				}
				if name == "phone" {
					if n := countDigits(m); n < 9 || n > 15 {
						return m
					}
				}
				found = true
				return "[" + strings.ToUpper(name) + "]"
			})
			if found {
				labels = append(labels, "pii."+name)
			}
		}
		if len(labels) == 0 {
			return sdk.GuardResult{Action: sdk.ActionAllow}, nil
		}
		return sdk.GuardResult{
			Action:   sdk.ActionRedact,
			Redacted: text,
			Reason:   "Redacted " + strings.Join(labels, ", "),
			Labels:   labels,
		}, nil
	}, nil
}

func countDigits(s string) int {
	n := 0
	for _, r := range s {
		if r >= '0' && r <= '9' {
			n++
		}
	}
	return n
}

func luhn(s string) bool {
	var digits []int
	for _, r := range s {
		if r >= '0' && r <= '9' {
			digits = append(digits, int(r-'0'))
		}
	}
	if len(digits) < 13 {
		return false
	}
	sum := 0
	for i := len(digits) - 1; i >= 0; i-- {
		d := digits[i]
		if (len(digits)-1-i)%2 == 1 {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
	}
	return sum%10 == 0
}
