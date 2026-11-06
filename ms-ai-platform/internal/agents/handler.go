// Package agents serves the agent library: agents that any orchestration can use.
package agents

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"agentic-platform/ms-ai-platform/internal/httpx"
	"agentic-platform/ms-ai-platform/internal/spec"
	"agentic-platform/ms-ai-platform/internal/store"
)

type Handler struct {
	Store     *store.Store
	Validator *spec.Validator
}

type agentInput struct {
	spec.Agent
	UpdatedByName string `json:"updatedByName"`
}

func (in *agentInput) validate(w http.ResponseWriter) bool {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		httpx.WriteError(w, http.StatusUnprocessableEntity, "invalid", "name is required", map[string]string{"field": "name"})
		return false
	}
	return true
}

func (h *Handler) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/agents", h.list)
	mux.HandleFunc("POST /v1/agents", h.create)
	mux.HandleFunc("POST /v1/agents/validate", h.validate)
	mux.HandleFunc("GET /v1/agents/{id}", h.get)
	mux.HandleFunc("PUT /v1/agents/{id}", h.update)
	mux.HandleFunc("DELETE /v1/agents/{id}", h.delete)
}

func writeStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "agent not found", nil)
		return
	}
	httpx.WriteError(w, http.StatusInternalServerError, "internal", err.Error(), nil)
}

func etag(version int) string { return fmt.Sprintf(`"%d"`, version) }

func writeAgent(w http.ResponseWriter, status int, a store.Agent) {
	w.Header().Set("ETag", etag(a.Version))
	httpx.WriteJSON(w, status, a)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	out, err := h.Store.ListAgents(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	a, err := h.Store.GetAgent(r.Context(), r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeAgent(w, http.StatusOK, a)
}

// create adds an agent to the library. Fields left out get the defaults of a
// new agent (the default LLM gateway and its default model).
func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var in agentInput
	if !httpx.DecodeJSON(w, r, &in) || !in.validate(w) {
		return
	}
	in.ID = httpx.NewID("agt")
	if in.Model.Asset == "" {
		in.Model = spec.AssetRef{Asset: spec.DefaultLLM, Config: []byte(`{}`)}
	}
	a, err := h.Store.CreateAgent(r.Context(), in.Agent, in.UpdatedByName)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeAgent(w, http.StatusCreated, a)
}

// update saves the agent with optimistic concurrency: the client must send the
// version it edited in If-Match. A stale version gets 409 with the current agent.
func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	match := strings.Trim(strings.TrimPrefix(r.Header.Get("If-Match"), "W/"), `"`)
	if match == "" {
		httpx.WriteError(w, http.StatusPreconditionRequired, "if_match_required", "send If-Match with the agent version you edited", nil)
		return
	}
	version, err := strconv.Atoi(match)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_if_match", "If-Match must be an agent version", nil)
		return
	}
	var in agentInput
	if !httpx.DecodeJSON(w, r, &in) || !in.validate(w) {
		return
	}
	a, err := h.Store.SaveAgent(r.Context(), r.PathValue("id"), version, in.Agent, in.UpdatedByName)
	if errors.Is(err, store.ErrVersionConflict) {
		w.Header().Set("ETag", etag(a.Version))
		by := a.UpdatedByName
		if by == "" {
			by = "someone else"
		}
		httpx.WriteJSON(w, http.StatusConflict, map[string]any{
			"error": httpx.Error{Code: "version_conflict", Message: fmt.Sprintf("Agent %s was changed by %s since you loaded it", a.Name, by)},
			"agent": a,
		})
		return
	}
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeAgent(w, http.StatusOK, a)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	usedBy, err := h.Store.DeleteAgent(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrInUse) {
		names := make([]string, len(usedBy))
		for i, o := range usedBy {
			names[i] = o.Name
		}
		httpx.WriteError(w, http.StatusConflict, "in_use",
			"remove the agent from "+strings.Join(names, ", ")+" before deleting it", map[string]any{"usedBy": usedBy})
		return
	}
	if err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// validate checks one agent on its own, as the root of an orchestration with
// no other agents.
func (h *Handler) validate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Agent spec.Agent `json:"agent"`
	}
	if !httpx.DecodeJSON(w, r, &in) {
		return
	}
	if in.Agent.ID == "" {
		in.Agent.ID = "agent"
	}
	o := spec.Orchestration{RootAgent: in.Agent.ID, Agents: []spec.Agent{in.Agent}}
	o.Normalize()
	knownSkill, err := h.Store.KnownSkills(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	problems := h.Validator.Validate(&o, knownSkill)
	if problems == nil {
		problems = []spec.Problem{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"valid": !spec.HasErrors(problems), "problems": problems})
}
