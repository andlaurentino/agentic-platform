package agui

import (
	"encoding/json"
	"strings"

	"google.golang.org/adk/session"

	"agentic-platform/ms-ai-platform/internal/guardrails"
	"agentic-platform/ms-ai-platform/internal/httpx"
)

const transferTool = "transfer_to_agent"

// Translator turns ADK session events into AG-UI events for one run.
// Steps are named by spec agent id so the UI can highlight canvas nodes.
type Translator struct {
	threadID, runID string
	agentIDs        map[string]string // runtime agent name -> spec agent id
	newID           func(prefix string) string

	step     string
	msgID    string // open text message, if any
	streamed strings.Builder
}

func NewTranslator(threadID, runID string, agentIDs map[string]string) *Translator {
	return &Translator{threadID: threadID, runID: runID, agentIDs: agentIDs, newID: httpx.NewID}
}

func (t *Translator) Start() []Event {
	return []Event{{Type: RunStarted, ThreadID: t.threadID, RunID: t.runID}}
}

func (t *Translator) stepFor(author string) string {
	if id, ok := t.agentIDs[author]; ok {
		return id
	}
	return author
}

func (t *Translator) closeText(final string) []Event {
	if t.msgID == "" {
		return nil
	}
	var out []Event
	if final != "" && final != t.streamed.String() {
		out = append(out, Event{Type: Custom, Name: CustomMessageFinal, Value: map[string]string{"messageId": t.msgID, "text": final}})
	}
	out = append(out, Event{Type: TextMessageEnd, MessageID: t.msgID})
	t.msgID = ""
	t.streamed.Reset()
	return out
}

func (t *Translator) switchStep(author string) []Event {
	step := t.stepFor(author)
	if step == t.step {
		return nil
	}
	out := t.closeText("")
	if t.step != "" {
		out = append(out, Event{Type: StepFinished, StepName: t.step})
	}
	t.step = step
	return append(out, Event{Type: StepStarted, StepName: step})
}

// Event translates one ADK event.
func (t *Translator) Event(ev *session.Event) []Event {
	if ev == nil || ev.Author == "user" || ev.Author == "" {
		return nil
	}
	out := t.switchStep(ev.Author)
	if ev.Content == nil {
		return out
	}

	var text strings.Builder
	for _, p := range ev.Content.Parts {
		if p != nil && !p.Thought && p.Text != "" {
			text.WriteString(p.Text)
		}
	}

	if ev.Partial {
		if text.Len() == 0 {
			return out
		}
		if t.msgID == "" {
			t.msgID = t.newID("msg")
			out = append(out, Event{Type: TextMessageStart, MessageID: t.msgID, Role: "assistant"})
		}
		t.streamed.WriteString(text.String())
		return append(out, Event{Type: TextMessageContent, MessageID: t.msgID, Delta: text.String()})
	}

	// Final event: finish the streamed message, or send the whole text if nothing streamed
	// (non-streaming models, or a response produced by a guardrail callback).
	parentMsg := t.msgID
	if t.msgID != "" {
		out = append(out, t.closeText(text.String())...)
	} else if text.Len() > 0 {
		parentMsg = t.newID("msg")
		out = append(out,
			Event{Type: TextMessageStart, MessageID: parentMsg, Role: "assistant"},
			Event{Type: TextMessageContent, MessageID: parentMsg, Delta: text.String()},
			Event{Type: TextMessageEnd, MessageID: parentMsg},
		)
	}

	for _, p := range ev.Content.Parts {
		if p == nil {
			continue
		}
		if fc := p.FunctionCall; fc != nil && fc.Name != transferTool {
			args, _ := json.Marshal(fc.Args)
			out = append(out,
				Event{Type: ToolCallStart, ToolCallID: fc.ID, ToolCallName: fc.Name, ParentMessageID: parentMsg},
				Event{Type: ToolCallArgs, ToolCallID: fc.ID, Delta: string(args)},
				Event{Type: ToolCallEnd, ToolCallID: fc.ID},
			)
		}
		if fr := p.FunctionResponse; fr != nil && fr.Name != transferTool {
			content, _ := json.Marshal(fr.Response)
			out = append(out, Event{Type: ToolCallResult, MessageID: t.newID("msg"), ToolCallID: fr.ID, Content: string(content), Role: "tool"})
		}
	}
	return out
}

// Decision reports a guardrail decision.
func (t *Translator) Decision(d guardrails.Decision) []Event {
	return []Event{{Type: Custom, Name: CustomGuardrailDecision, Value: d}}
}

func (t *Translator) finishSteps() []Event {
	out := t.closeText("")
	if t.step != "" {
		out = append(out, Event{Type: StepFinished, StepName: t.step})
		t.step = ""
	}
	return out
}

func (t *Translator) Finish() []Event {
	return append(t.finishSteps(), Event{Type: RunFinished, ThreadID: t.threadID, RunID: t.runID})
}

func (t *Translator) Error(code, msg string) []Event {
	return append(t.finishSteps(), Event{Type: RunError, Code: code, Message: msg})
}
