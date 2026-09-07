package httptransport

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestTrustedProxyPolicyResolvesClientFromRightmostUntrustedHop(t *testing.T) {
	t.Parallel()
	policy, err := NewTrustedProxyPolicy("10.0.0.0/8,fd00::/8")
	if err != nil {
		t.Fatalf("NewTrustedProxyPolicy() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "http://internal/api/v1/auth/sessions", nil)
	request.RemoteAddr = "10.0.0.2:44321"
	request.Header.Set("X-Forwarded-Proto", "https")
	request.Header.Set("X-Forwarded-For", "198.51.100.8, 203.0.113.7, 10.0.0.1")

	address, err := policy.ClientIP(request)
	if err != nil {
		t.Fatalf("ClientIP() error = %v", err)
	}
	if want := netip.MustParseAddr("203.0.113.7"); address != want {
		t.Fatalf("ClientIP() = %s, want %s", address, want)
	}
}

func TestTrustedProxyPolicyIgnoresForwardedHeadersOnDirectTLS(t *testing.T) {
	t.Parallel()
	policy, err := NewTrustedProxyPolicy("10.0.0.0/8")
	if err != nil {
		t.Fatalf("NewTrustedProxyPolicy() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "https://nexus.example.test/api/v1/auth/session", nil)
	request.RemoteAddr = "203.0.113.9:51234"
	request.Header.Set("X-Forwarded-Proto", "http")
	request.Header.Set("X-Forwarded-For", "198.51.100.1")

	address, err := policy.ClientIP(request)
	if err != nil || address != netip.MustParseAddr("203.0.113.9") {
		t.Fatalf("ClientIP() = %s, %v", address, err)
	}
}

func TestTrustedProxyPolicyRejectsUntrustedOrMalformedPlaintextHops(t *testing.T) {
	t.Parallel()
	policy, err := NewTrustedProxyPolicy("10.0.0.0/8")
	if err != nil {
		t.Fatalf("NewTrustedProxyPolicy() error = %v", err)
	}
	tests := []struct {
		name       string
		remoteAddr string
		proto      string
		forwarded  string
		want       error
	}{
		{name: "untrusted peer", remoteAddr: "203.0.113.9:80", proto: "https", forwarded: "198.51.100.1", want: ErrSecureTransportRequired},
		{name: "missing https", remoteAddr: "10.0.0.2:80", proto: "http", forwarded: "198.51.100.1", want: ErrSecureTransportRequired},
		{name: "missing chain", remoteAddr: "10.0.0.2:80", proto: "https", want: ErrInvalidProxyChain},
		{name: "multiple proto values", remoteAddr: "10.0.0.2:80", proto: "https, http", forwarded: "198.51.100.1", want: ErrSecureTransportRequired},
		{name: "noncanonical IP", remoteAddr: "10.0.0.2:80", proto: "https", forwarded: "2001:0db8::1", want: ErrInvalidProxyChain},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "http://internal/api/v1/auth/session", nil)
			request.RemoteAddr = test.remoteAddr
			request.Header.Set("X-Forwarded-Proto", test.proto)
			if test.forwarded != "" {
				request.Header.Set("X-Forwarded-For", test.forwarded)
			}
			if _, err := policy.ClientIP(request); !errors.Is(err, test.want) {
				t.Fatalf("ClientIP() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestTrustedProxyPolicyRejectsRepeatedForwardedProtoHeader(t *testing.T) {
	t.Parallel()
	policy, err := NewTrustedProxyPolicy("10.0.0.0/8")
	if err != nil {
		t.Fatalf("NewTrustedProxyPolicy() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://internal/api/v1/auth/session", nil)
	request.RemoteAddr = "10.0.0.2:80"
	request.Header.Add("X-Forwarded-Proto", "https")
	request.Header.Add("X-Forwarded-Proto", "http")
	request.Header.Set("X-Forwarded-For", "198.51.100.1")
	if _, err := policy.ClientIP(request); !errors.Is(err, ErrSecureTransportRequired) {
		t.Fatalf("ClientIP() error = %v", err)
	}
}

func TestTrustedProxyPolicyRequiresCanonicalUniqueCIDRs(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"", "10.0.0.1/8", "10.0.0.0/8,10.0.0.0/8", "not-a-cidr"} {
		if _, err := NewTrustedProxyPolicy(value); err == nil {
			t.Fatalf("NewTrustedProxyPolicy(%q) error = nil", value)
		}
	}
}
