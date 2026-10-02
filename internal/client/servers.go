package client

import (
	"context"
	"errors"
	"net/http"
	"net/url"
)

// CreateJavaServer creates a server using Crafty's download API.
func (c *Client) CreateJavaServer(ctx context.Context, request CreateJavaServerRequest) (CreatedServer, error) {
	var result CreatedServer
	err := c.request(ctx, http.MethodPost, "/api/v2/servers", request, &result)
	return result, err
}

// ListServers returns servers visible to the authenticated token.
// Both GET endpoints return server data in 4.10.4. The collection allows
// reconciliation without interpreting single GET HTTP 400 NOT_AUTHORIZED.
func (c *Client) ListServers(ctx context.Context) ([]Server, error) {
	var result []Server
	err := c.request(ctx, http.MethodGet, "/api/v2/servers", nil, &result)
	return result, err
}

// UpdateServer updates a server's name.
func (c *Client) UpdateServer(ctx context.Context, id string, request UpdateServerRequest) error {
	return c.request(ctx, http.MethodPatch, "/api/v2/servers/"+url.PathEscape(id), request, nil)
}

// DeleteServer deletes a server and returns the API result unchanged.
func (c *Client) DeleteServer(ctx context.Context, id string) error {
	return c.request(ctx, http.MethodDelete, "/api/v2/servers/"+url.PathEscape(id), nil, nil)
}

// IsNotFound reports an HTTP 404 error; callers decide whether it is idempotent.
func IsNotFound(err error) bool {
	var api *APIError
	return errors.As(err, &api) && api.StatusCode == http.StatusNotFound
}
