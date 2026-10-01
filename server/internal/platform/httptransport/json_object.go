package httptransport

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/laugh0608/RadishNexus/server/internal/platform/authz"
	"io"
	"net/http"
)

// Decode a bounded object with an exact field set, retaining explicit null and
// rejecting duplicate keys before command-specific field validation.
func decodeStrictObject(w http.ResponseWriter, r *http.Request, fields []string, maxBytes int64) (map[string]json.RawMessage, error) {
	var raw json.RawMessage
	if err := decodeJSON(w, r, &raw, maxBytes); err != nil {
		return nil, err
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return nil, authz.ErrInvalid
	}
	values := map[string]json.RawMessage{}
	for d.More() {
		token, err = d.Token()
		if err != nil {
			return nil, authz.ErrInvalid
		}
		key, ok := token.(string)
		if !ok {
			return nil, authz.ErrInvalid
		}
		if _, exists := values[key]; exists {
			return nil, authz.ErrInvalid
		}
		var value json.RawMessage
		if err = d.Decode(&value); err != nil {
			return nil, authz.ErrInvalid
		}
		values[key] = value
	}
	if _, err = d.Token(); err != nil {
		return nil, authz.ErrInvalid
	}
	if _, err = d.Token(); !errors.Is(err, io.EOF) {
		return nil, authz.ErrInvalid
	}
	if len(values) != len(fields) {
		return nil, authz.ErrInvalid
	}
	for _, field := range fields {
		if _, exists := values[field]; !exists {
			return nil, authz.ErrInvalid
		}
	}
	return values, nil
}
