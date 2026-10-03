package provider

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bart-kochanowicz/terraform-provider-crafty/internal/client"
)

const pendingRefreshKey = "pending_refresh"

var errIncompleteServer = errors.New("server response is missing required name, automatic-start, monitoring, or execution-command fields")

// waitRefresh only repeats GET requests. Three consecutive successful absences
// confirm removal of an established resource; a newly created/pending resource
// stays in state until it becomes visible or the operation times out.
func (s *serverResource) waitRefresh(ctx context.Context, m *serverModel, pending bool, expected *client.UpdateServerRequest) (bool, error) {
	delay := s.pollInterval
	if delay <= 0 {
		delay = time.Second
	}
	missing := 0
	var observation error
	for {
		if err := ctx.Err(); err != nil {
			return false, fmt.Errorf("waiting for server %s: %w (last observation: %v)", m.ID.ValueString(), err, observation)
		}
		candidate := *m
		found, err := s.refresh(ctx, &candidate)
		if err == nil && found && candidate.matchesPatch(expected) {
			*m = candidate
			return true, nil
		}
		if err != nil {
			missing = 0
			if !client.IsRetryableRead(err) && (!pending || !errors.Is(err, errIncompleteServer)) {
				return false, err
			}
			observation = err
		} else if !found {
			missing++
			if !pending && missing >= 3 {
				return false, nil
			}
			observation = fmt.Errorf("server is not yet visible in the collection")
		} else {
			missing = 0
			observation = fmt.Errorf("server settings have not converged to the requested values")
		}
		wait := delay
		var api *client.APIError
		if errors.As(err, &api) && api.RetryAfter > wait {
			wait = api.RetryAfter
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
		delay = min(delay*2, 10*time.Second)
	}
}

func (s *serverResource) waitDeleted(ctx context.Context, m *serverModel) error {
	// Read only identity here: a server with incomplete metadata still exists.
	// Require three consecutive successful absences, as with established reads.
	delay := s.pollInterval
	if delay <= 0 {
		delay = time.Second
	}
	missing := 0
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("waiting for deletion of %s: %w", m.ID.ValueString(), err)
		}
		servers, err := s.client.ListServers(ctx)
		if err == nil {
			found := false
			for _, remote := range servers {
				if remote.ID == m.ID.ValueString() {
					found = true
					break
				}
			}
			if found {
				missing = 0
			} else {
				missing++
				if missing >= 3 {
					return nil
				}
			}
		} else {
			missing = 0
			if !client.IsRetryableRead(err) {
				return err
			}
		}
		wait := delay
		var api *client.APIError
		if errors.As(err, &api) && api.RetryAfter > wait {
			wait = api.RetryAfter
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
		delay = min(delay*2, 10*time.Second)
	}
}
