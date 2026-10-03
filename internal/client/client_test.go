package client

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientHTTPDiagnostics(t *testing.T) {
	for _, code := range []int{401, 403, 404, 500} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer secret" {
					t.Error("missing bearer token")
				}
				w.WriteHeader(code)
			}))
			defer server.Close()
			c := &Client{base: server.URL, token: "secret", http: server.Client()}
			err := c.request(context.Background(), http.MethodGet, "/api/v2/servers", nil, nil)
			var api *APIError
			if !errors.As(err, &api) || api.StatusCode != code {
				t.Fatalf("expected HTTP %d error, got %v", code, err)
			}
		})
	}
}
func TestClientRejectsUnsuccessfulResponses(t *testing.T) {
	for _, body := range []string{`{"status":"error","error":"SECRET"}`, `broken`, `{}`, `{"status":"ok"}`} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
			defer server.Close()
			c := &Client{base: server.URL, http: server.Client()}
			var data []any
			if err := c.request(context.Background(), "GET", "/api/v2/servers", nil, &data); err == nil {
				t.Fatal("expected response error")
			}
		})
	}
}
func TestClientDoesNotFollowRedirects(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { t.Error("redirect must not be followed") }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer server.Close()
	c := New(server.URL, "secret")
	if err := c.request(context.Background(), "GET", "/api/v2/servers", nil, nil); err == nil {
		t.Fatal("expected redirect rejection")
	}
}

func TestClientRejectsUntrustedTLS(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("untrusted TLS request reached handler") }))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	defer server.Close()
	err := New(server.URL, "secret-token").request(context.Background(), "GET", "/api/v2/servers", nil, nil)
	var authority x509.UnknownAuthorityError
	if !errors.As(err, &authority) {
		t.Fatalf("expected certificate rejection, got %v", err)
	}
	if strings.Contains(err.Error(), "secret-token") {
		t.Fatal("token leaked in TLS diagnostic")
	}
}

func TestClientResponseSizeBoundary(t *testing.T) {
	const limit = 8 * 1024 * 1024
	body := `{"status":"ok","data":[]}`
	for _, size := range []int{limit, limit + 1} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, body+strings.Repeat(" ", size-len(body)))
			}))
			defer server.Close()
			var result []any
			err := New(server.URL, "test-only").request(context.Background(), "GET", "/api/v2/servers", nil, &result)
			if size == limit && err != nil {
				t.Fatalf("valid response at limit rejected: %v", err)
			}
			if size > limit && (err == nil || !strings.Contains(err.Error(), "exceeds 8 MiB")) {
				t.Fatalf("oversize response accepted: %v", err)
			}
		})
	}
}

func TestClientDiagnosticsExcludeSensitiveResponse(t *testing.T) {
	for _, tc := range []struct {
		name string
		code int
		body string
	}{
		{"http", 403, "secret-body secret-token"},
		{"application", 200, `{"status":"error","error":"secret-body secret-token"}`},
		{"malformed", 200, `{"secret-body secret-token":`},
		{"invalid data", 200, `{"status":"ok","data":"secret-body secret-token"}`},
		{"null data", 200, `{"status":"ok","data":null,"extra":"secret-body secret-token"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.code)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			var result []Server
			err := New(server.URL, "secret-token").request(context.Background(), "GET", "/api/v2/servers", nil, &result)
			if err == nil {
				t.Fatal("expected response failure")
			}
			if strings.Contains(err.Error(), "secret-body") || strings.Contains(err.Error(), "secret-token") {
				t.Fatalf("sensitive data leaked: %v", err)
			}
		})
	}
}
