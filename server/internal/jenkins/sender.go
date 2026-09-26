package jenkins

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"time"
)

type SendResult struct {
	CIRunID   string `json:"ci_run_id"`
	Duplicate bool   `json:"duplicate"`
}

var ErrDelivery = errors.New("delivery_failed")
var ErrResponse = errors.New("invalid_delivery_response")

// Send performs only this explicit delivery, with a fixed total budget. The
// supplied client's roots may support a private CA; TLS verification is required.
func Send(ctx context.Context, c SenderConfig, body []byte, client *http.Client) (SendResult, error) {
	return send(ctx, c, body, client, time.Now, wait)
}
func wait(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
func send(ctx context.Context, c SenderConfig, body []byte, client *http.Client, now func() time.Time, sleep func(context.Context, time.Duration) error) (SendResult, error) {
	if !validSender(c) || len(c.secret) != 32 {
		return SendResult{}, ErrConfiguration
	}
	p, err := ParsePayload(body, now())
	if err != nil {
		return SendResult{}, err
	}
	if client == nil {
		transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}}
		defer transport.CloseIdleConnections()
		client = &http.Client{Transport: transport}
	}
	if tr, ok := client.Transport.(*http.Transport); ok && tr.TLSClientConfig != nil && tr.TLSClientConfig.InsecureSkipVerify {
		return SendResult{}, ErrConfiguration
	}
	httpClient := *client
	httpClient.Timeout = 10 * time.Second
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	for attempt := 0; attempt < 4; attempt++ {
		if ctx.Err() != nil {
			return SendResult{}, ErrDelivery
		}
		timestamp := strconv.FormatInt(now().Unix(), 10)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Origin+Path(c.SourceID), bytes.NewReader(body))
		if err != nil {
			return SendResult{}, ErrConfiguration
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Nexus-Key-ID", c.KeyID)
		req.Header.Set("X-Nexus-Delivery-ID", p.DeliveryID())
		req.Header.Set("X-Nexus-Timestamp", timestamp)
		req.Header.Set("X-Nexus-Signature", signature(c.secret, c.SourceID, c.KeyID, p.DeliveryID(), timestamp, body))
		response, requestErr := httpClient.Do(req)
		delay := time.Second * time.Duration(1<<attempt)
		retry := requestErr != nil
		if requestErr == nil {
			raw, readErr := io.ReadAll(io.LimitReader(response.Body, 4097))
			_ = response.Body.Close()
			if response.StatusCode == 200 || response.StatusCode == 201 {
				if readErr != nil {
					retry = true
				} else {
					result, err := parseResponse(raw, response)
					return result, err
				}
			} else {
				switch response.StatusCode {
				case 408, 429, 500, 502, 503, 504:
					retry = true
				}
				if !retry {
					return SendResult{}, responseError(raw)
				}
			}
			if value := response.Header.Get("Retry-After"); value != "" {
				if secs, e := strconv.Atoi(value); e == nil && strconv.Itoa(secs) == value && secs >= 1 && secs <= 10 {
					delay = time.Duration(secs) * time.Second
				}
			}
		}
		if !retry || attempt == 3 {
			return SendResult{}, ErrDelivery
		}
		if sleep(ctx, delay) != nil {
			return SendResult{}, ErrDelivery
		}
	}
	return SendResult{}, ErrDelivery
}
func parseResponse(body []byte, r *http.Response) (SendResult, error) {
	var result SendResult
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || len(body) > 4096 {
		return result, ErrResponse
	}
	root, err := object(body, "data")
	if err != nil {
		return result, ErrResponse
	}
	fields, err := object(root["data"], "ci_run_id", "duplicate")
	if err != nil {
		return result, ErrResponse
	}
	if decode(fields["ci_run_id"], &result.CIRunID) != nil || decode(fields["duplicate"], &result.Duplicate) != nil || !ciIDPattern.MatchString(result.CIRunID) || result.Duplicate != (r.StatusCode == 200) {
		return SendResult{}, ErrResponse
	}
	return result, nil
}
func responseError(body []byte) error {
	// Only protocol machine codes cross the process output boundary.
	var response struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if len(body) <= 4096 && json.Unmarshal(body, &response) == nil {
		switch response.Error.Code {
		case "unauthenticated", "invalid_request", "unsupported_result", "run_not_completed", "source_binding_mismatch", "conflict", "not_found", "secure_transport_required", "invalid_origin", "invalid_proxy_chain", "method_not_allowed", "payload_too_large", "unsupported_media_type":
			return errors.New(response.Error.Code)
		}
	}
	return ErrDelivery
}
