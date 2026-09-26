package jenkins

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func senderFixture(t *testing.T, origin string) SenderConfig {
	t.Helper()
	c, e := LoadSender([]byte(fmt.Sprintf(`{"version":1,"origin":%q,"source_id":"source_a","key_id":"key_a","secret_file":"/synthetic/key"}`, origin)), testRead)
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func TestSenderRetryBudgetsAndStableIdentity(t *testing.T) {
	for _, code := range []int{0, 408, 429, 500, 502, 503, 504} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			c := senderFixture(t, "https://nexus.example")
			source := testSource(t)
			calls := 0
			clock := testNow
			var waits []time.Duration
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				raw, _ := io.ReadAll(r.Body)
				if string(raw) != testPayload || r.Header.Get("X-Nexus-Delivery-ID") != "build-42" || !source.Verify(r.Header.Get("X-Nexus-Key-ID"), r.Header.Get("X-Nexus-Delivery-ID"), r.Header.Get("X-Nexus-Timestamp"), r.Header.Get("X-Nexus-Signature"), raw, clock) {
					t.Fatal("retry signature or identity")
				}
				deadline, ok := r.Context().Deadline()
				if !ok || time.Until(deadline) > 10*time.Second {
					t.Fatal("missing request deadline")
				}
				if code == 0 {
					return nil, errors.New("synthetic sensitive network detail")
				}
				return &http.Response{StatusCode: code, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":{"code":"temporary"}}`))}, nil
			})}
			_, e := send(context.Background(), c, []byte(testPayload), client, func() time.Time { return clock }, func(_ context.Context, d time.Duration) error {
				waits = append(waits, d)
				clock = clock.Add(d)
				return nil
			})
			if e != ErrDelivery || calls != 4 || fmt.Sprint(waits) != "[1s 2s 4s]" {
				t.Fatal(e, calls, waits)
			}
		})
	}
}
func TestSenderRetryAfterAndCancellation(t *testing.T) {
	for _, value := range []string{"3", "0", "11", "03", "Wed, 21 Oct 2015 07:28:00 GMT"} {
		c := senderFixture(t, "https://nexus.example")
		calls := 0
		var delay time.Duration
		ctx, cancel := context.WithCancel(context.Background())
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": []string{value}}, Body: io.NopCloser(strings.NewReader(""))}, nil
		})}
		_, e := send(ctx, c, []byte(testPayload), client, func() time.Time { return testNow }, func(ctx context.Context, d time.Duration) error { delay = d; cancel(); return ctx.Err() })
		want := time.Second
		if value == "3" {
			want = 3 * time.Second
		}
		if e != ErrDelivery || calls != 1 || delay != want {
			t.Fatal(value, e, calls, delay)
		}
		cancel()
	}
}
func TestSenderStopsOnPermanentErrorsAndMalformedSuccess(t *testing.T) {
	for _, tc := range []struct {
		code int
		body string
		want string
	}{
		{401, `{"error":{"code":"unauthenticated","message":"secret"}}`, "unauthenticated"},
		{409, `{"error":{"code":"conflict"}}`, "conflict"},
		{403, `{"error":{"code":"secret"}}`, "delivery_failed"},
		{200, `{"data":{"ci_run_id":"cir_test","duplicate":false}}`, "invalid_delivery_response"},
		{201, `{"data":{"ci_run_id":"cir_test","duplicate":false,"secret":"x"}}`, "invalid_delivery_response"},
		{201, `{"data":{"ci_run_id":"https://secret","duplicate":false}}`, "invalid_delivery_response"},
		{201, strings.Repeat("x", 4097), "invalid_delivery_response"},
	} {
		calls := 0
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: tc.code, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
		})}
		_, e := send(context.Background(), senderFixture(t, "https://nexus.example"), []byte(testPayload), client, func() time.Time { return testNow }, wait)
		if e == nil || e.Error() != tc.want || calls != 1 {
			t.Fatal(tc.code, e, calls)
		}
	}
}
func TestSenderTLSRedirectAndCertificateVerification(t *testing.T) {
	redirected := 0
	target := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected++ }))
	defer target.Close()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer server.Close()
	c := senderFixture(t, server.URL)
	if _, e := send(context.Background(), c, []byte(testPayload), server.Client(), func() time.Time { return testNow }, wait); e != ErrDelivery || redirected != 0 {
		t.Fatal(e, redirected)
	}
	calls := 0
	if _, e := send(context.Background(), c, []byte(testPayload), &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: x509.NewCertPool()}}}, func() time.Time { return testNow }, func(context.Context, time.Duration) error { calls++; return nil }); e != ErrDelivery || calls != 3 {
		t.Fatal("untrusted cert accepted", e)
	}
	insecure := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	if _, e := send(context.Background(), c, []byte(testPayload), insecure, func() time.Time { return testNow }, wait); e != ErrConfiguration {
		t.Fatal(e)
	}
}
