package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
)

var jsonMarshal = json.Marshal

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

type apiError struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
}

func fail(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, apiError{Error: msg, Code: code})
}

// readJSON decodes a JSON body of at most max bytes.
func readJSON(r *http.Request, max int64, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, max))
	if err := dec.Decode(v); err != nil {
		return errors.New("invalid JSON body")
	}
	return nil
}

var (
	reID     = regexp.MustCompile(`^[a-z2-7]{12}$`) // library.ID: base32 lower, 12 chars
	reCover  = regexp.MustCompile(`^[0-9a-f]{16}$`)
	reSID    = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)
	reSource = regexp.MustCompile(`^[a-z0-9:_-]{1,32}$`)
)
