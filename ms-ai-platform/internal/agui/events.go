// Package agui streams agent runs to clients using the AG-UI protocol
// (https://docs.ag-ui.com): the client POSTs a RunAgentInput and receives
// typed JSON events over Server-Sent Events.
package agui

import (
	"encoding/json"
	"net/http"
	"sync"
)

// EventType values follow @ag-ui/core.
type EventType string

const (
	RunStarted         EventType = "RUN_STARTED"
	RunFinished        EventType = "RUN_FINISHED"
	RunError           EventType = "RUN_ERROR"
	StepStarted        EventType = "STEP_STARTED"
	StepFinished       EventType = "STEP_FINISHED"
	TextMessageStart   EventType = "TEXT_MESSAGE_START"
	TextMessageContent EventType = "TEXT_MESSAGE_CONTENT"
	TextMessageEnd     EventType = "TEXT_MESSAGE_END"
	ToolCallStart      EventType = "TOOL_CALL_START"
	ToolCallArgs       EventType = "TOOL_CALL_ARGS"
	ToolCallEnd        EventType = "TOOL_CALL_END"
	ToolCallResult     EventType = "TOOL_CALL_RESULT"
	Custom             EventType = "CUSTOM"
)

// Custom event names this platform emits.
const (
	CustomGuardrailDecision = "guardrail.decision"
	// CustomMessageFinal carries the final text of a streamed message when it
	// differs from the concatenated deltas (e.g. after output redaction).
	CustomMessageFinal = "message.final"
)

// Event is one AG-UI event. Only the fields relevant to Type are set.
type Event struct {
	Type            EventType `json:"type"`
	ThreadID        string    `json:"threadId,omitempty"`
	RunID           string    `json:"runId,omitempty"`
	StepName        string    `json:"stepName,omitempty"`
	MessageID       string    `json:"messageId,omitempty"`
	Role            string    `json:"role,omitempty"`
	Delta           string    `json:"delta,omitempty"`
	ToolCallID      string    `json:"toolCallId,omitempty"`
	ToolCallName    string    `json:"toolCallName,omitempty"`
	ParentMessageID string    `json:"parentMessageId,omitempty"`
	Content         string    `json:"content,omitempty"`
	Message         string    `json:"message,omitempty"`
	Code            string    `json:"code,omitempty"`
	Name            string    `json:"name,omitempty"`
	Value           any       `json:"value,omitempty"`
}

// RunAgentInput is the request body of an AG-UI run.
type RunAgentInput struct {
	ThreadID       string          `json:"threadId"`
	RunID          string          `json:"runId"`
	Messages       []Message       `json:"messages"`
	Tools          []any           `json:"tools"`
	Context        []any           `json:"context"`
	State          json.RawMessage `json:"state"`
	ForwardedProps json.RawMessage `json:"forwardedProps"`
}

// Message is an AG-UI message. Content is a string for user and assistant messages.
type Message struct {
	ID      string          `json:"id"`
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// Text returns the message content as plain text.
func (m Message) Text() string {
	var s string
	if json.Unmarshal(m.Content, &s) == nil {
		return s
	}
	// Multimodal content: keep only text parts.
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(m.Content, &parts) == nil {
		for _, p := range parts {
			if p.Type == "text" {
				s += p.Text
			}
		}
	}
	return s
}

// sseWriter writes events as SSE. Safe for concurrent use (heartbeats run in a goroutine).
type sseWriter struct {
	mu sync.Mutex
	w  http.ResponseWriter
	rc *http.ResponseController
}

func newSSEWriter(w http.ResponseWriter) *sseWriter {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	s := &sseWriter{w: w, rc: http.NewResponseController(w)}
	_ = s.rc.Flush()
	return s
}

func (s *sseWriter) write(events ...Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ev := range events {
		b, err := json.Marshal(ev)
		if err != nil {
			return err
		}
		if _, err := s.w.Write([]byte("data: ")); err != nil {
			return err
		}
		if _, err := s.w.Write(b); err != nil {
			return err
		}
		if _, err := s.w.Write([]byte("\n\n")); err != nil {
			return err
		}
	}
	return s.rc.Flush()
}

func (s *sseWriter) ping() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.w.Write([]byte(": ping\n\n")); err != nil {
		return err
	}
	return s.rc.Flush()
}
