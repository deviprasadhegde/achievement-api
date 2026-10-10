package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/deviprasadhegde/achievement-api/internal/logging"
)

// Every error response in the API has exactly this shape (see
// docs/api-contract.md §5), so clients need only one error handler.
type errorEnvelope struct {
	Error apiError `json:"error"`
}

type apiError struct {
	Code      string       `json:"code"`
	Message   string       `json:"message"`
	Details   []FieldError `json:"details,omitempty"`
	RequestID string       `json:"requestId,omitempty"`
}

// FieldError describes one invalid input field.
type FieldError struct {
	Field string `json:"field"`
	Issue string `json:"issue"`
}

// Error codes from the API contract.
const (
	CodeValidation         = "VALIDATION_ERROR"
	CodeUnauthorized       = "UNAUTHORIZED"
	CodeNotFound           = "NOT_FOUND"
	CodeMethodNotAllowed   = "METHOD_NOT_ALLOWED"
	CodeRateLimited        = "RATE_LIMITED"
	CodeInternal           = "INTERNAL_ERROR"
	CodeServiceUnavailable = "SERVICE_UNAVAILABLE"
)

// writeJSON encodes v and writes it with the given status.
//
// We marshal into memory *before* writing the status line: once WriteHeader
// has been called you can no longer change the status, so if encoding failed
// halfway the client would get a "200" with broken JSON.
func writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		logging.FromContext(r.Context()).Error("encode response", "err", err)
		status = http.StatusInternalServerError
		body = []byte(`{"error":{"code":"INTERNAL_ERROR","message":"internal server error"}}`)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if _, err := w.Write(append(body, '\n')); err != nil {
		// The client went away; nothing more we can do except note it.
		logging.FromContext(r.Context()).Debug("write response", "err", err)
	}
}

// writeError writes a contract-shaped error, always including the request ID
// so a user can quote it and we can find the matching log lines.
func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string, details []FieldError) {
	writeJSON(w, r, status, errorEnvelope{Error: apiError{
		Code:      code,
		Message:   message,
		Details:   details,
		RequestID: RequestIDFrom(r.Context()),
	}})
}
