package client

import (
	"context"
	"net/http"
	"net/url"
)

// WebhookSettings is the mutable webhook configuration accepted by Crafty.
type WebhookSettings struct {
	Type     string   `json:"webhook_type"`
	Name     string   `json:"name"`
	URL      string   `json:"url"`
	BotName  string   `json:"bot_name"`
	Triggers []string `json:"trigger"`
	Body     string   `json:"body"`
	Color    string   `json:"color"`
	Enabled  bool     `json:"enabled"`
}

// Webhook contains the collection response; triggers are comma-separated on reads.
type Webhook struct {
	Type     *string `json:"webhook_type"`
	Name     *string `json:"name"`
	URL      *string `json:"url"`
	BotName  *string `json:"bot_name"`
	Triggers *string `json:"trigger"`
	Body     *string `json:"body"`
	Color    *string `json:"color"`
	Enabled  *bool   `json:"enabled"`
}

func webhookPath(serverID string) string {
	return "/api/v2/servers/" + url.PathEscape(serverID) + "/webhook"
}

// ListWebhooks returns webhooks after Crafty's server CONFIG permission check.
func (c *Client) ListWebhooks(ctx context.Context, serverID string) (map[string]Webhook, error) {
	var result map[string]Webhook
	err := c.request(ctx, http.MethodGet, webhookPath(serverID), nil, &result)
	return result, err
}

// CreateWebhook returns the identity assigned by Crafty without replaying the POST.
func (c *Client) CreateWebhook(ctx context.Context, serverID string, settings WebhookSettings) (int64, error) {
	var result struct {
		ID int64 `json:"webhook_id"`
	}
	err := c.request(ctx, http.MethodPost, webhookPath(serverID), settings, &result)
	return result.ID, err
}

// UpdateWebhook replaces configuration without sending a test notification.
func (c *Client) UpdateWebhook(ctx context.Context, serverID, id string, settings WebhookSettings) error {
	return c.request(ctx, http.MethodPatch, webhookPath(serverID)+"/"+url.PathEscape(id), settings, nil)
}

// DeleteWebhook removes a webhook configuration.
func (c *Client) DeleteWebhook(ctx context.Context, serverID, id string) error {
	return c.request(ctx, http.MethodDelete, webhookPath(serverID)+"/"+url.PathEscape(id), nil, nil)
}
