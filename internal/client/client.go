// Package client implements the Crafty Controller v2 HTTP API.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// New creates an authenticated client with verified TLS and no redirects.
func New(baseURL, token string) *Client {
	return &Client{base: strings.TrimRight(baseURL, "/"), token: token, http: &http.Client{
		Timeout:       120 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

// Client sends authenticated requests to Crafty.
type Client struct {
	base, token string
	http        *http.Client
}

// APIError describes an unsuccessful HTTP response without exposing its body.
type APIError struct {
	StatusCode   int
	RetryAfter   time.Duration
	method, path string
}

func (e *APIError) Error() string {
	hint := "The Crafty API rejected the request."
	switch e.StatusCode {
	case 401:
		hint = "Authentication failed. Check the API token."
	case 403:
		hint = "Access denied. Check the API token and server permissions."
	case 404:
		hint = "The requested server or API endpoint was not found."
	}
	return fmt.Sprintf("%s %s returned HTTP %d. %s", e.method, e.path, e.StatusCode, hint)
}
func (c *Client) send(ctx context.Context, method, path string, body any) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode API request: %w", err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return nil, fmt.Errorf("create API request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("crafty API transport failed: %w", err)
	}
	defer func() {
		// Closing a response body does not affect the completed API operation.
		_ = resp.Body.Close()
	}()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &APIError{StatusCode: resp.StatusCode, method: method, path: path, RetryAfter: retryAfter(resp.Header.Get("Retry-After"))}
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024+1))
	if err != nil {
		return nil, fmt.Errorf("read API response: %w", err)
	}
	if len(b) > 8*1024*1024 {
		return nil, fmt.Errorf("API response exceeds 8 MiB")
	}
	return b, nil
}

func (c *Client) request(ctx context.Context, method, path string, body, result any) error {
	b, err := c.send(ctx, method, path, body)
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(b)) == 0 {
		if result != nil {
			return fmt.Errorf("API returned an empty response")
		}
		return nil
	}
	var envelope struct {
		Status string          `json:"status"`
		Data   json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(b, &envelope); err != nil {
		return fmt.Errorf("decode API response: %w", err)
	}
	// Do not include response bodies: they can contain sensitive information.
	if envelope.Status != "ok" {
		return fmt.Errorf("crafty API returned an unsuccessful application status")
	}
	if result != nil {
		if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
			return fmt.Errorf("API response is missing data")
		}
		if err := json.Unmarshal(envelope.Data, result); err != nil {
			return fmt.Errorf("decode API data: %w", err)
		}
	}
	return nil
}

// Retry-After can be a non-negative delay in seconds or an HTTP date.
func retryAfter(value string) time.Duration {
	if seconds, err := strconv.ParseUint(value, 10, 32); err == nil {
		return time.Duration(seconds) * time.Second
	}
	if date, err := http.ParseTime(value); err == nil {
		if delay := time.Until(date); delay > 0 {
			return delay
		}
	}
	return 0
}
