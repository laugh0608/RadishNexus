package contextpackage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var tokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var operationPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func ValidOperation(s string) bool { return operationPattern.MatchString(s) }
func ValidID(s, prefix string) bool {
	return prefix != "" && len(s) > len(prefix) && len(s) <= 128 && strings.HasPrefix(s, prefix) && tokenPattern.MatchString(s)
}
func invalid(stage string) error { return fmt.Errorf("%w: %s", ErrInvalid, stage) }
func validTime(t time.Time) bool { _, offset := t.Zone(); return !t.IsZero() && offset == 0 }
func textValue(s string, max int, nonempty bool) bool {
	return utf8.ValidString(s) && len(s) <= max && !strings.ContainsRune(s, 0) && (!nonempty || strings.TrimSpace(s) != "")
}
func label(s string, max int) bool {
	return textValue(s, max, true) && strings.IndexFunc(s, unicode.IsControl) < 0
}
func oneOf(s string, values ...string) bool {
	for _, v := range values {
		if v == s {
			return true
		}
	}
	return false
}

// Decode rejects duplicate keys at every level, excessive nesting, unknown
// fields and trailing data. Errors never echo attacker-controlled input.
func Decode(raw []byte, target any) error {
	if !utf8.Valid(raw) {
		return invalid("UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := jsonValue(d, 0); err != nil {
		return invalid("JSON shape")
	}
	if _, err := d.Token(); err != io.EOF {
		return invalid("trailing JSON")
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return invalid("JSON fields")
	}
	return nil
}
func jsonValue(d *json.Decoder, depth int) error {
	if depth > 48 {
		return ErrInvalid
	}
	t, err := d.Token()
	if err != nil {
		return err
	}
	delimiter, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			k, e := d.Token()
			if e != nil {
				return e
			}
			key, ok := k.(string)
			if !ok || seen[key] {
				return ErrInvalid
			}
			seen[key] = true
			if e = jsonValue(d, depth+1); e != nil {
				return e
			}
		}
	case '[':
		for d.More() {
			if e := jsonValue(d, depth+1); e != nil {
				return e
			}
		}
	default:
		return ErrInvalid
	}
	end, err := d.Token()
	if err != nil {
		return err
	}
	if (delimiter == '{' && end != json.Delim('}')) || (delimiter == '[' && end != json.Delim(']')) {
		return ErrInvalid
	}
	return nil
}
func validateLineage(items []Origin) bool {
	if items == nil || len(items) > MaxLineage {
		return false
	}
	seen := map[string]bool{}
	for _, o := range items {
		key := o.PackageID + "/" + o.SourceID
		if !ValidID(o.PackageID, "pkg_") || !digestPattern.MatchString(o.Digest) || !tokenPattern.MatchString(o.SourceID) || len(o.SourceID) > 128 || seen[key] {
			return false
		}
		seen[key] = true
	}
	return true
}
