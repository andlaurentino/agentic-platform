// Package skills serves the skills REST API.
package skills

import (
	"errors"
	"net/http"
	"strings"

	"agentic-platform/ms-ai-platform/internal/httpx"
	"agentic-platform/ms-ai-platform/internal/store"
)

type Handler struct {
	Store *store.Store
}

type skillInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Content     string `json:"content"`
}

func (in skillInput) validate(w http.ResponseWriter) bool {
	if strings.TrimSpace(in.Name) == "" {
		httpx.WriteError(w, http.StatusUnprocessableEntity, "invalid", "name is required", map[string]string{"field": "name"})
		return false
	}
	return true
}

func (h *Handler) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/skills", h.list)
	mux.HandleFunc("POST /v1/skills", h.create)
	mux.HandleFunc("GET /v1/skills/{id}", h.get)
	mux.HandleFunc("PUT /v1/skills/{id}", h.update)
	mux.HandleFunc("DELETE /v1/skills/{id}", h.delete)
}

func writeStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "skill not found", nil)
		return
	}
	httpx.WriteError(w, http.StatusInternalServerError, "internal", err.Error(), nil)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	out, err := h.Store.ListSkills(r.Context())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	s, err := h.Store.GetSkill(r.Context(), r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, s)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var in skillInput
	if !httpx.DecodeJSON(w, r, &in) || !in.validate(w) {
		return
	}
	s, err := h.Store.CreateSkill(r.Context(), store.Skill{ID: httpx.NewID("skl"), Name: strings.TrimSpace(in.Name), Description: in.Description, Content: in.Content})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, s)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	var in skillInput
	if !httpx.DecodeJSON(w, r, &in) || !in.validate(w) {
		return
	}
	s, err := h.Store.UpdateSkill(r.Context(), store.Skill{ID: r.PathValue("id"), Name: strings.TrimSpace(in.Name), Description: in.Description, Content: in.Content})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, s)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	if err := h.Store.DeleteSkill(r.Context(), r.PathValue("id")); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
