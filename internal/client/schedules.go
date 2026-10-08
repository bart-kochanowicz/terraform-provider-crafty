package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// ScheduleSettings configures an independent recurring task.
type ScheduleSettings struct {
	Name         string `json:"name"`
	Enabled      bool   `json:"enabled"`
	Action       string `json:"action"`
	Command      string `json:"command"`
	Interval     int64  `json:"interval"`
	IntervalType string `json:"interval_type"`
	StartTime    string `json:"start_time"`
	Cron         string `json:"cron_string"`
	ActionID     string `json:"action_id"`
	OneTime      bool   `json:"one_time"`
	Parent       *int64 `json:"parent"`
	Delay        int64  `json:"delay"`
}

// Schedule is the raw individual GET response, including its owning server.
type Schedule struct {
	ID     int64 `json:"schedule_id"`
	Server *struct {
		ID string `json:"server_id"`
	} `json:"server_id"`
	Name         *string `json:"name"`
	Enabled      *bool   `json:"enabled"`
	Action       *string `json:"action"`
	Command      *string `json:"command"`
	Interval     *int64  `json:"interval"`
	IntervalType *string `json:"interval_type"`
	StartTime    *string `json:"start_time"`
	Cron         *string `json:"cron_string"`
	ActionID     *string `json:"action_id"`
	OneTime      *bool   `json:"one_time"`
	Parent       *int64  `json:"parent"`
	Delay        *int64  `json:"delay"`
}

func schedulePath(serverID string) string {
	return "/api/v2/servers/" + url.PathEscape(serverID) + "/tasks"
}

// GetSchedule reads a raw JSON object, unlike Crafty's usual response envelope.
func (c *Client) GetSchedule(ctx context.Context, serverID, id string) (Schedule, error) {
	var result Schedule
	body, err := c.send(ctx, http.MethodGet, schedulePath(serverID)+"/"+url.PathEscape(id), nil)
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return result, fmt.Errorf("decode schedule response: %w", err)
	}
	if result.Server == nil || result.Server.ID != serverID || fmt.Sprint(result.ID) != id {
		return result, fmt.Errorf("schedule response does not match the requested server and task")
	}
	return result, nil
}

// CreateSchedule returns the assigned identity without replaying creation.
func (c *Client) CreateSchedule(ctx context.Context, serverID string, settings ScheduleSettings) (int64, error) {
	var result struct {
		ID int64 `json:"schedule_id"`
	}
	err := c.request(ctx, http.MethodPost, schedulePath(serverID), settings, &result)
	return result.ID, err
}

// UpdateSchedule changes task configuration without running the task.
func (c *Client) UpdateSchedule(ctx context.Context, serverID, id string, settings ScheduleSettings) error {
	return c.request(ctx, http.MethodPatch, schedulePath(serverID)+"/"+url.PathEscape(id), settings, nil)
}

// DeleteSchedule removes the persisted task and its scheduler job.
func (c *Client) DeleteSchedule(ctx context.Context, serverID, id string) error {
	return c.request(ctx, http.MethodDelete, schedulePath(serverID)+"/"+url.PathEscape(id), nil, nil)
}
