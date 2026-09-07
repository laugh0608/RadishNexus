package httptransport

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

type TrustedProxyPolicy struct {
	trusted []netip.Prefix
}

func NewTrustedProxyPolicy(value string) (TrustedProxyPolicy, error) {
	if strings.TrimSpace(value) == "" {
		return TrustedProxyPolicy{}, fmt.Errorf("trusted proxy CIDRs are required")
	}
	parts := strings.Split(value, ",")
	prefixes := make([]netip.Prefix, 0, len(parts))
	seen := make(map[netip.Prefix]struct{}, len(parts))
	for _, part := range parts {
		canonical := strings.TrimSpace(part)
		prefix, err := netip.ParsePrefix(canonical)
		if err != nil {
			return TrustedProxyPolicy{}, fmt.Errorf("parse trusted proxy CIDR %q: %w", canonical, err)
		}
		prefix = prefix.Masked()
		if prefix.String() != canonical {
			return TrustedProxyPolicy{}, fmt.Errorf("trusted proxy CIDR %q must be canonical", canonical)
		}
		if _, exists := seen[prefix]; exists {
			return TrustedProxyPolicy{}, fmt.Errorf("trusted proxy CIDR %q is duplicated", canonical)
		}
		seen[prefix] = struct{}{}
		prefixes = append(prefixes, prefix)
	}
	return TrustedProxyPolicy{trusted: prefixes}, nil
}

// ClientIP accepts a direct TLS request, or a plaintext hop from an explicitly
// trusted TLS-terminating proxy. Forwarded headers from direct TLS callers are
// ignored; plaintext callers must provide one exact proto value and a complete
// X-Forwarded-For chain.
func (policy TrustedProxyPolicy) ClientIP(request *http.Request) (netip.Addr, error) {
	peer, err := remoteIP(request.RemoteAddr)
	if err != nil {
		return netip.Addr{}, ErrInvalidProxyChain
	}
	if request.TLS != nil {
		return peer, nil
	}
	if !policy.isTrusted(peer) {
		return netip.Addr{}, ErrSecureTransportRequired
	}
	forwardedProto := request.Header.Values("X-Forwarded-Proto")
	if len(forwardedProto) != 1 || forwardedProto[0] != "https" {
		return netip.Addr{}, ErrSecureTransportRequired
	}

	forwardedFor := request.Header.Values("X-Forwarded-For")
	if len(forwardedFor) != 1 || strings.TrimSpace(forwardedFor[0]) == "" {
		return netip.Addr{}, ErrInvalidProxyChain
	}
	parts := strings.Split(forwardedFor[0], ",")
	chain := make([]netip.Addr, 0, len(parts))
	for _, part := range parts {
		canonical := strings.TrimSpace(part)
		address, parseErr := netip.ParseAddr(canonical)
		if parseErr != nil || address.String() != canonical {
			return netip.Addr{}, ErrInvalidProxyChain
		}
		chain = append(chain, address.Unmap())
	}
	for index := len(chain) - 1; index >= 0; index-- {
		if !policy.isTrusted(chain[index]) {
			return chain[index], nil
		}
	}
	return chain[0], nil
}

func (policy TrustedProxyPolicy) isTrusted(address netip.Addr) bool {
	address = address.Unmap()
	for _, prefix := range policy.trusted {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func remoteIP(remoteAddress string) (netip.Addr, error) {
	host, _, err := net.SplitHostPort(remoteAddress)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("parse remote address: %w", err)
	}
	address, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("parse remote IP: %w", err)
	}
	return address.Unmap(), nil
}
