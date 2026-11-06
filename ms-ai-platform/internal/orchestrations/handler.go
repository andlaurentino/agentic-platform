// Package orchestrations serves orchestrations and their shared draft.
package orchestrations

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

func (h *Handler) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/orchestrations", h.list)
	mux.HandleFunc("POST /v1/orchestrations", h.create)
	mux.HandleFunc("GET /v1/orchestrations/{id}", h.get)
	mux.HandleFunc("DELETE /v1/orchestrations/{id}", h.delete)
	mux.HandleFunc("GET /v1/orchestrations/{id}/draft", h.getDraft)
	mux.HandleFunc("PUT /v1/orchestrations/{id}/draft", h.putDraft)
	mux.HandleFunc("POST /v1/orchestrations/{id}/draft/validate", h.validate)
}

func writeStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "orchestration not found", nil)
		return
	}
	httpx.WriteError(w, http.StatusInternalServerError, "internal", err.Error(), nil)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	out, err := h.Store.ListOrchestrations(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	o, err := h.Store.GetOrchestration(r.Context(), r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, o)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if !httpx.DecodeJSON(w, r, &in) {
		return
	}
	if strings.TrimSpace(in.Name) == "" {
		httpx.WriteError(w, http.StatusUnprocessableEntity, "invalid", "name is required", map[string]string{"field": "name"})
		return
	}
	graph, starter := spec.NewStarter(httpx.NewID("agt"))
	o, err := h.Store.CreateOrchestration(r.Context(),
		store.Orchestration{ID: httpx.NewID("orc"), Name: strings.TrimSpace(in.Name), Description: in.Description},
		graph, starter)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, o)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	if err := h.Store.DeleteOrchestration(r.Context(), r.PathValue("id")); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func etag(version int) string { return fmt.Sprintf(`"%d"`, version) }

func writeDraft(w http.ResponseWriter, status int, d store.Draft) {
	w.Header().Set("ETag", etag(d.Version))
	httpx.WriteJSON(w, status, d)
}

func (h *Handler) getDraft(w http.ResponseWriter, r *http.Request) {
	d, err := h.Store.GetDraft(r.Context(), r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeDraft(w, http.StatusOK, d)
}

// putDraft saves the draft's graph with optimistic concurrency: the client must
// send the version it edited in If-Match. A stale version gets 409 with the
// current draft. Agents are saved through /v1/agents.
func (h *Handler) putDraft(w http.ResponseWriter, r *http.Request) {
	match := strings.Trim(strings.TrimPrefix(r.Header.Get("If-Match"), "W/"), `"`)
	if match == "" {
		httpx.WriteError(w, http.StatusPreconditionRequired, "if_match_required", "send If-Match with the draft version you edited", nil)
		return
	}
	version, err := strconv.Atoi(match)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_if_match", "If-Match must be a draft version", nil)
		return
	}
	var in struct {
		Spec          spec.Graph `json:"spec"`
		UpdatedByName string     `json:"updatedByName"`
	}
	if !httpx.DecodeJSON(w, r, &in) {
		return
	}
	d, err := h.Store.SaveDraft(r.Context(), r.PathValue("id"), version, in.Spec, in.UpdatedByName)
	if errors.Is(err, store.ErrUnknownAgent) {
		httpx.WriteError(w, http.StatusUnprocessableEntity, "unknown_agent", err.Error(), nil)
		return
	}
	if errors.Is(err, store.ErrVersionConflict) {
		w.Header().Set("ETag", etag(d.Version))
		httpx.WriteJSON(w, http.StatusConflict, map[string]any{
			"error": httpx.Error{Code: "version_conflict", Message: fmt.Sprintf("The draft was changed by %s since you loaded it", nameOr(d.UpdatedByName))},
			"draft": d,
		})
		return
	}
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeDraft(w, http.StatusOK, d)
}

func nameOr(name string) string {
	if name == "" {
		return "someone else"
	}
	return name
}

// validate checks the spec in the body (agents included), or the stored draft
// when the body is empty.
func (h *Handler) validate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Spec *spec.Orchestration `json:"spec"`
	}
	if !httpx.DecodeJSON(w, r, &in) {
		return
	}
	o := in.Spec
	if o == nil {
		resolved, err := h.Store.ResolveDraft(r.Context(), r.PathValue("id"))
		if err != nil {
			writeStoreError(w, err)
			return
		}
		o = &resolved
	}
	o.Normalize()
	knownSkill, err := h.Store.KnownSkills(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	problems := h.Validator.Validate(o, knownSkill)
	if problems == nil {
		problems = []spec.Problem{}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"valid": !spec.HasErrors(problems), "problems": problems})
}
