package authn

import (
	"strings"
	"testing"
)

func TestEmailCredentialPolicy(t *testing.T) {
	for _, value := range []string{"admin", "a@localhost", "a b@example.test", ".a@example.test", "a.@example.test", "a..b@example.test", "a@example..test", "a@-example.test", "a@example-.test", "a@exa_mple.test", "名字@example.test", "K@example.test", `"a"@example.test`, "a@[127.0.0.1]", strings.Repeat("a", 65) + "@example.test", "a@" + strings.Repeat("a", 64) + ".test"} {
		if _, err := NormalizeEmail(value); err == nil {
			t.Errorf("accepted invalid mailbox %q", value)
		}
	}
	if got, err := NormalizeEmail("  Admin+Nexus@Example.Test  "); err != nil || got != "admin+nexus@example.test" {
		t.Fatalf("NormalizeEmail() = %q, %v", got, err)
	}
}
