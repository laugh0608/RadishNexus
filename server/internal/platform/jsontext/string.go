package jsontext

import (
	"encoding/json"
	"errors"
	"strconv"
	"unicode/utf8"
)

var errInvalidUnicode = errors.New("invalid Unicode in JSON string")

// Decode preserves authoritative text by rejecting malformed UTF-8 and unpaired
// JSON surrogate escapes before encoding/json can replace them with U+FFFD.
func Decode(raw []byte, dest *string) error {
	if !utf8.Valid(raw) {
		return errInvalidUnicode
	}
	for i := 0; i < len(raw); i++ {
		if raw[i] != '\\' {
			continue
		}
		i++
		if i >= len(raw) {
			return errInvalidUnicode
		}
		if raw[i] != 'u' {
			continue
		}
		if i+4 >= len(raw) {
			return errInvalidUnicode
		}
		code, err := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
		if err != nil {
			return errInvalidUnicode
		}
		i += 4
		if code >= 0xDC00 && code <= 0xDFFF {
			return errInvalidUnicode
		}
		if code >= 0xD800 && code <= 0xDBFF {
			if i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
				return errInvalidUnicode
			}
			low, err := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
			if err != nil || low < 0xDC00 || low > 0xDFFF {
				return errInvalidUnicode
			}
			i += 6
		}
	}
	return json.Unmarshal(raw, dest)
}
