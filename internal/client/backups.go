package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// BackupConfig contains the readable settings of an existing backup policy.
type BackupConfig struct {
	ID           string          `json:"backup_id"`
	ServerID     string          `json:"server_id"`
	Name         *string         `json:"backup_name"`
	Location     *string         `json:"backup_location"`
	MaxBackups   *int64          `json:"max_backups"`
	Compress     *bool           `json:"compress"`
	Shutdown     *bool           `json:"shutdown"`
	Before       *string         `json:"before"`
	After        *string         `json:"after"`
	ExcludedDirs json.RawMessage `json:"excluded_dirs"`
}

// BackupConfigPatch omits settings not managed by Terraform.
type BackupConfigPatch struct {
	Name         *string   `json:"backup_name,omitempty"`
	Location     *string   `json:"backup_location,omitempty"`
	MaxBackups   *int64    `json:"max_backups,omitempty"`
	Compress     *bool     `json:"compress,omitempty"`
	Shutdown     *bool     `json:"shutdown,omitempty"`
	Before       *string   `json:"before,omitempty"`
	After        *string   `json:"after,omitempty"`
	ExcludedDirs *[]string `json:"excluded_dirs,omitempty"`
}

// ListBackupConfigs reads the raw collection returned after the BACKUP permission check.
func (c *Client) ListBackupConfigs(ctx context.Context, serverID string) (map[string]BackupConfig, error) {
	var result map[string]BackupConfig
	body, err := c.send(ctx, http.MethodGet, "/api/v2/servers/"+url.PathEscape(serverID)+"/backups", nil)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("decode backup configuration response: %w", err)
	}
	if result == nil {
		return nil, fmt.Errorf("backup configuration response is null")
	}
	return result, nil
}

// UpdateBackupConfig changes configuration without creating or running a backup.
func (c *Client) UpdateBackupConfig(ctx context.Context, serverID, id string, patch BackupConfigPatch) error {
	return c.request(ctx, http.MethodPatch, "/api/v2/servers/"+url.PathEscape(serverID)+"/backups/backup/"+url.PathEscape(id), patch, nil)
}
