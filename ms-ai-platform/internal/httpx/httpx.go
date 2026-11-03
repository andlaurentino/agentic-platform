// Package httpx has small HTTP helpers shared by handlers.
package httpx

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Error is the JSON error envelope.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func WriteError(w http.ResponseWriter, status int, code, msg string, details any) {
	WriteJSON(w, status, map[string]Error{"error": {Code: code, Message: msg, Details: details}})
}

// DecodeJSON reads a JSON body (max 2 MiB) into v.
func DecodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, 2<<20))
	if err := dec.Decode(v); err != nil && !errors.Is(err, io.EOF) {
		WriteError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return false
	}
	return true
}

const idAlphabet = "abcdefghijkmnpqrstuvwxyz23456789"

// NewID returns a prefixed random id such as "orc_7k2m9x4pqa".
func NewID(prefix string) string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	var sb strings.Builder
	sb.WriteString(prefix)
	sb.WriteByte('_')
	for _, c := range b {
		sb.WriteByte(idAlphabet[int(c)%len(idAlphabet)])
	}
	return sb.String()
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController reach Flush on the real writer (needed for SSE).
func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// Logging logs one line per request.
func Logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		slog.Info("request", "method", r.Method, "path", r.URL.Path, "status", sw.status, "dur", time.Since(start).Round(time.Millisecond))
	})
}
