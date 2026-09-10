// Package radishoidc owns the single, explicitly configured Radish provider.
package radishoidc

import (
	"crypto/sha256"
	"errors"
	"net/url"
	"strings"
	"unicode"
)

// Config contains reviewed server configuration. It must never be serialized
// into the public methods endpoint or a browser bundle.
type Config struct {
	Issuer           string
	ClientID         string
	ClientSecretFile string
	CAFile           string
	RedirectURI      string
}

func LoadConfig(getenv func(string) string, publicOrigin string) (*Config, error) {
	issuer, clientID, secretFile, caFile := getenv("RADISHNEXUS_OIDC_ISSUER"), getenv("RADISHNEXUS_OIDC_CLIENT_ID"), getenv("RADISHNEXUS_OIDC_CLIENT_SECRET_FILE"), getenv("RADISHNEXUS_OIDC_CA_FILE")
	if issuer == "" && clientID == "" && secretFile == "" && caFile == "" {
		return nil, nil
	}
	if issuer == "" || clientID == "" || secretFile == "" {
		return nil, errors.New("OIDC requires issuer, client ID and client secret file")
	}
	parsed, err := url.Parse(issuer)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.RawFragment != "" || parsed.Opaque != "" || parsed.Host != strings.ToLower(parsed.Host) || parsed.String() != issuer || len(issuer) > 2048 {
		return nil, errors.New("OIDC issuer must be an exact HTTPS URL without credentials, query or fragment")
	}
	if len(clientID) > 128 || strings.TrimSpace(clientID) != clientID || strings.IndexFunc(clientID, unicode.IsControl) >= 0 {
		return nil, errors.New("invalid OIDC client ID")
	}
	origin, err := url.Parse(publicOrigin)
	if err != nil || origin.Scheme != "https" || origin.Host == "" || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.ForceQuery || origin.Fragment != "" {
		return nil, errors.New("OIDC requires an exact HTTPS public origin")
	}
	return &Config{Issuer: issuer, ClientID: clientID, ClientSecretFile: secretFile, CAFile: caFile, RedirectURI: publicOrigin + "/api/v1/auth/oidc/callback"}, nil
}

func (config Config) fingerprint() []byte {
	digest := sha256.Sum256([]byte(config.Issuer + "\x00" + config.ClientID + "\x00" + config.RedirectURI + "\x00openid profile\x00RS256\x00client_secret_post"))
	return digest[:]
}
