package agui

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"google.golang.org/adk/agent"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/genai"

	"agentic-platform/ms-ai-platform/internal/compiler"
	"agentic-platform/ms-ai-platform/internal/guardrails"
	"agentic-platform/ms-ai-platform/internal/httpx"
	"agentic-platform/ms-ai-platform/internal/spec"
	"agentic-platform/ms-ai-platform/internal/store"
)

// Source loads what a run needs.
type Source interface {
	ResolveDraft(ctx context.Context, orchestrationID string) (spec.Orchestration, error)
	ListSkills(ctx context.Context) ([]store.Skill, error)
}

// Handler serves POST /v1/orchestrations/{id}/agui.
type Handler struct {
	Source   Source
	Compiler interface {
		Compile(ctx context.Context, o spec.Orchestration, skills map[string]store.Skill, run *guardrails.Run) (*compiler.Compiled, error)
	}
	// Sessions keeps conversation history between runs of the same thread.
	Sessions  session.Service
	Heartbeat time.Duration
}

const userID = "anonymous" // no authentication in v1

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	orchID := r.PathValue("id")
	if target := r.URL.Query().Get("target"); target != "" && target != "draft" {
		httpx.WriteError(w, http.StatusBadRequest, "unsupported_target", "only target=draft is supported in this version", nil)
		return
	}
	var in RunAgentInput
	if !httpx.DecodeJSON(w, r, &in) {
		return
	}
	if in.ThreadID == "" {
		in.ThreadID = httpx.NewID("thr")
	}
	if in.RunID == "" {
		in.RunID = httpx.NewID("run")
	}
	var userText string
	for i := len(in.Messages) - 1; i >= 0; i-- {
		if in.Messages[i].Role == "user" {
			userText = in.Messages[i].Text()
			break
		}
	}

	ctx := r.Context()
	orch, err := h.Source.ResolveDraft(ctx, orchID)
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "orchestration not found", nil)
		return
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "internal", err.Error(), nil)
		return
	}

	sse := newSSEWriter(w)
	stopPing := h.startHeartbeat(sse)
	defer stopPing()

	tr := NewTranslator(in.ThreadID, in.RunID, nil)
	send := func(events []Event) bool {
		if err := sse.write(events...); err != nil {
			slog.Debug("client went away", "err", err)
			return false
		}
		return true
	}
	if !send(tr.Start()) {
		return
	}
	if userText == "" {
		send(tr.Error("no_user_message", "the run needs a user message"))
		return
	}

	skillList, err := h.Source.ListSkills(ctx)
	if err != nil {
		send(tr.Error("internal", err.Error()))
		return
	}
	skills := make(map[string]store.Skill, len(skillList))
	for _, s := range skillList {
		skills[s.ID] = s
	}

	decisions := &decisionBuffer{}
	compiled, err := h.Compiler.Compile(ctx, orch, skills, guardrails.NewRun(decisions.add))
	if err != nil {
		code := "compile_failed"
		var ve *compiler.ValidationError
		if errors.As(err, &ve) {
			code = "invalid_orchestration"
		}
		send(tr.Error(code, err.Error()))
		return
	}
	defer compiled.Close()
	tr.agentIDs = compiled.AgentIDs

	run, err := runner.New(runner.Config{
		AppName:           "orchestration:" + orchID,
		Agent:             compiled.Root,
		SessionService:    h.Sessions,
		AutoCreateSession: true,
	})
	if err != nil {
		send(tr.Error("internal", err.Error()))
		return
	}

	msg := genai.NewContentFromText(userText, genai.RoleUser)
	for ev, err := range run.Run(ctx, userID, in.ThreadID, msg, agent.RunConfig{StreamingMode: agent.StreamingModeSSE}) {
		for _, d := range decisions.drain() {
			if !send(tr.Decision(d)) {
				return
			}
		}
		if err != nil {
			slog.Warn("run failed", "orchestration", orchID, "thread", in.ThreadID, "err", err)
			send(tr.Error("run_failed", err.Error()))
			return
		}
		if !send(tr.Event(ev)) {
			return
		}
	}
	for _, d := range decisions.drain() {
		send(tr.Decision(d))
	}
	send(tr.Finish())
}

func (h *Handler) startHeartbeat(sse *sseWriter) func() {
	every := h.Heartbeat
	if every <= 0 {
		every = 15 * time.Second
	}
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				if sse.ping() != nil {
					return
				}
			}
		}
	}()
	return func() { close(done) }
}

type decisionBuffer struct {
	mu  sync.Mutex
	buf []guardrails.Decision
}

func (b *decisionBuffer) add(d guardrails.Decision) {
	b.mu.Lock()
	b.buf = append(b.buf, d)
	b.mu.Unlock()
}

func (b *decisionBuffer) drain() []guardrails.Decision {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := b.buf
	b.buf = nil
	return out
}
