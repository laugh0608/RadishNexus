package runtimeconfig

import (
	"errors"
	"strings"
	"testing"
)

func TestSetupCodeFile(t *testing.T) {
	for _, tc := range []struct {
		name, path, body       string
		readErr, errorExpected bool
	}{
		{name: "disabled"}, {name: "valid", path: "/run/secrets/setup", body: strings.Repeat("A", 43) + "\n"},
		{name: "relative", path: "secret", errorExpected: true}, {name: "missing", path: "/private-secret", readErr: true, errorExpected: true},
		{name: "empty", path: "/secret", errorExpected: true}, {name: "multiline", path: "/secret", body: strings.Repeat("A", 43) + "\nX", errorExpected: true},
		{name: "too large", path: "/secret", body: strings.Repeat("A", 100), errorExpected: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := SetupCode(func(string) string { return tc.path }, func(string) ([]byte, error) {
				if tc.readErr {
					return nil, errors.New("private-secret")
				}
				return []byte(tc.body), nil
			})
			if (err != nil) != tc.errorExpected {
				t.Fatal(err)
			}
			if err != nil && strings.Contains(err.Error(), "private-secret") {
				t.Fatal("secret path leaked")
			}
		})
	}
}
