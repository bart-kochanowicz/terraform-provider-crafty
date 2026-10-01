package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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
	c := &Client{base: server.URL, token: "secret", http: &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}
	if err := c.request(context.Background(), "GET", "/api/v2/servers", nil, nil); err == nil {
		t.Fatal("expected redirect rejection")
	}
}
