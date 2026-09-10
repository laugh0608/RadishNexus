package radishoidc

import (
	"strings"
	"testing"
)

func TestConfigurationIsExplicitAndClosed(t *testing.T) {
	getenv := func(values map[string]string) func(string) string {
		return func(key string) string { return values[key] }
	}
	if config, err := LoadConfig(getenv(nil), "https://nexus.example.test"); err != nil || config != nil {
		t.Fatal("unconfigured provider should be absent", err)
	}
	for _, values := range []map[string]string{
		{"RADISHNEXUS_OIDC_CLIENT_ID": "private"},
		{"RADISHNEXUS_OIDC_ISSUER": "http://radish.example.test", "RADISHNEXUS_OIDC_CLIENT_ID": "nexus", "RADISHNEXUS_OIDC_CLIENT_SECRET_FILE": "/test/secret"},
		{"RADISHNEXUS_OIDC_ISSUER": "https://private@radish.example.test", "RADISHNEXUS_OIDC_CLIENT_ID": "nexus", "RADISHNEXUS_OIDC_CLIENT_SECRET_FILE": "/test/secret"},
		{"RADISHNEXUS_OIDC_ISSUER": "https://radish.example.test?private", "RADISHNEXUS_OIDC_CLIENT_ID": "nexus", "RADISHNEXUS_OIDC_CLIENT_SECRET_FILE": "/test/secret"},
		{"RADISHNEXUS_OIDC_ISSUER": "https://radish.example.test", "RADISHNEXUS_OIDC_CLIENT_ID": "private\n", "RADISHNEXUS_OIDC_CLIENT_SECRET_FILE": "/test/secret"},
	} {
		if _, err := LoadConfig(getenv(values), "https://nexus.example.test"); err == nil || strings.Contains(err.Error(), "private") {
			t.Fatal("unsafe configuration accepted or leaked", err)
		}
	}
	config, err := LoadConfig(getenv(map[string]string{"RADISHNEXUS_OIDC_ISSUER": "https://radish.example.test/issuer/", "RADISHNEXUS_OIDC_CLIENT_ID": "nexus", "RADISHNEXUS_OIDC_CLIENT_SECRET_FILE": "/test/secret"}), "https://nexus.example.test")
	if err != nil || config.Issuer != "https://radish.example.test/issuer/" || config.RedirectURI != "https://nexus.example.test/api/v1/auth/oidc/callback" {
		t.Fatal("configuration changed exact issuer or redirect", err)
	}
}
