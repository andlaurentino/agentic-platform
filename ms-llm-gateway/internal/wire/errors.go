package wire

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// Error is an API error in the OpenAI shape: {"error": {message, type, code}}.
type Error struct {
	Status  int    `json:"-"`
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code,omitempty"`
	Param   string `json:"param,omitempty"`
}

func (e *Error) Error() string { return fmt.Sprintf("%d %s: %s", e.Status, e.Type, e.Message) }

// Errorf builds an Error.
func Errorf(status int, typ, code, format string, args ...any) *Error {
	return &Error{Status: status, Type: typ, Code: code, Message: fmt.Sprintf(format, args...)}
}

// BadRequest is a 400 invalid_request_error.
func BadRequest(format string, args ...any) *Error {
	return Errorf(http.StatusBadRequest, "invalid_request_error", "", format, args...)
}

// WriteError writes err as an OpenAI error response.
func WriteError(w http.ResponseWriter, err *Error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(err.Status)
	_ = json.NewEncoder(w).Encode(map[string]*Error{"error": err})
}

// WriteJSON writes v with status 200.
func WriteJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
