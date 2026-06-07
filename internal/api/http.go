// Package api exposes REST handlers and shared JSON helpers for the deploy API.
package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"kube-deploy/internal/model"
)

const maxRequestBodyBytes = 1 << 20 // 1 MiB

// httpError carries an HTTP status and message for writeError.
type httpError struct {
	status  int
	message string
}

func (e *httpError) Error() string { return e.message }

func badRequest(message string) error {
	return &httpError{status: http.StatusBadRequest, message: message}
}

func notFound(message string) error {
	return &httpError{status: http.StatusNotFound, message: message}
}

func unauthorized(message string) error {
	return &httpError{status: http.StatusUnauthorized, message: message}
}

// decodeJSON reads a single JSON object from the body and rejects unknown fields.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	if r.Body == nil {
		return badRequest("request body is required")
	}
	defer r.Body.Close()

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		var syntaxErr *json.SyntaxError
		var typeErr *json.UnmarshalTypeError
		var maxBytesErr *http.MaxBytesError
		switch {
		case errors.Is(err, io.EOF):
			return badRequest("request body is required")
		case errors.As(err, &maxBytesErr):
			return badRequest("request body must be 1 MiB or smaller")
		case errors.As(err, &syntaxErr), errors.As(err, &typeErr):
			return badRequest("invalid json")
		default:
			return badRequest("invalid json")
		}
	}
	// Reject trailing JSON after the first object.
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return badRequest("request body must be 1 MiB or smaller")
		}
		return badRequest("request body must contain a single json object")
	}
	return nil
}

// writeError maps httpError to JSON; other errors become 500.
func writeError(w http.ResponseWriter, err error) {
	var he *httpError
	if errors.As(err, &he) {
		writeJSON(w, he.status, model.ErrorResponse{Error: he.message})
		return
	}
	writeJSON(w, http.StatusInternalServerError, model.ErrorResponse{Error: "internal server error"})
}

// writeJSON sets Content-Type and encodes v as JSON with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
