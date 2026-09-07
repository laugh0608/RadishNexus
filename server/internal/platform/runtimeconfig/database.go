// Package runtimeconfig resolves process-level deployment configuration.
package runtimeconfig

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

const (
	databaseURLKey          = "DATABASE_URL"
	databasePasswordFileKey = "RADISHNEXUS_DATABASE_PASSWORD_FILE"
	maxDatabasePasswordSize = 1025
)

// DatabaseURL returns the configured PostgreSQL connection URL. A deployment
// may keep the non-secret address in DATABASE_URL and overlay its password from
// an absolute Compose secret path.
func DatabaseURL(
	getenv func(string) string,
	readFile func(string) ([]byte, error),
) (string, error) {
	configuredURL := getenv(databaseURLKey)
	if configuredURL == "" {
		return "", errors.New("DATABASE_URL is required")
	}
	passwordPath := getenv(databasePasswordFileKey)
	if passwordPath == "" {
		return configuredURL, nil
	}
	if !filepath.IsAbs(passwordPath) {
		return "", errors.New("RADISHNEXUS_DATABASE_PASSWORD_FILE must be an absolute path")
	}

	body, err := readFile(passwordPath)
	if err != nil {
		return "", fmt.Errorf("read RADISHNEXUS_DATABASE_PASSWORD_FILE: %w", err)
	}
	password, err := parseSecretLine(body)
	if err != nil {
		return "", fmt.Errorf("read RADISHNEXUS_DATABASE_PASSWORD_FILE: %w", err)
	}

	parsed, err := url.Parse(configuredURL)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		return "", errors.New("DATABASE_URL must be a PostgreSQL URL when RADISHNEXUS_DATABASE_PASSWORD_FILE is set")
	}
	if parsed.User == nil || parsed.User.Username() == "" {
		return "", errors.New("DATABASE_URL must include a database user when RADISHNEXUS_DATABASE_PASSWORD_FILE is set")
	}
	if _, present := parsed.User.Password(); present {
		return "", errors.New("DATABASE_URL must not embed a password when RADISHNEXUS_DATABASE_PASSWORD_FILE is set")
	}
	parsed.User = url.UserPassword(parsed.User.Username(), password)
	return parsed.String(), nil
}

func parseSecretLine(body []byte) (string, error) {
	if len(body) > maxDatabasePasswordSize {
		return "", errors.New("database password file exceeds 1025 bytes")
	}
	password := string(body)
	password = strings.TrimSuffix(password, "\n")
	password = strings.TrimSuffix(password, "\r")
	if password == "" {
		return "", errors.New("database password file is empty")
	}
	if strings.ContainsAny(password, "\r\n\x00") {
		return "", errors.New("database password file must contain exactly one non-empty line")
	}
	return password, nil
}
