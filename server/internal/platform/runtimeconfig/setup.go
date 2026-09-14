package runtimeconfig

import (
	"errors"
	"path/filepath"
	"strings"
)

// SetupCode reads an optional deployment-only secret; paths and file contents
// never appear in errors. Removing the configuration and restarting is supported.
func SetupCode(getenv func(string) string, readFile func(string) ([]byte, error)) (string, error) {
	path := getenv("RADISHNEXUS_SETUP_CODE_FILE")
	if path == "" {
		return "", nil
	}
	if !filepath.IsAbs(path) {
		return "", errors.New("RADISHNEXUS_SETUP_CODE_FILE must be an absolute path")
	}
	body, err := readFile(path)
	if err != nil {
		return "", errors.New("cannot read RADISHNEXUS_SETUP_CODE_FILE")
	}
	if len(body) > 45 {
		return "", errors.New("RADISHNEXUS_SETUP_CODE_FILE exceeds 45 bytes")
	}
	code := strings.TrimSuffix(strings.TrimSuffix(string(body), "\n"), "\r")
	if len(code) != 43 {
		return "", errors.New("RADISHNEXUS_SETUP_CODE_FILE must contain one 43-character base64url code")
	}
	return code, nil
}
