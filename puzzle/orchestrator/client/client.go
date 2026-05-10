package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"puzzle/orchestrator/game"
)

// Agent is a thin REST client that calls a single agent /invoke endpoint with retries.
type Agent struct {
	Name    string
	BaseURL string
	HTTP    *http.Client
}

func New(name, baseURL string, timeout time.Duration) *Agent {
	return &Agent{
		Name:    name,
		BaseURL: baseURL,
		HTTP:    &http.Client{Timeout: timeout},
	}
}

type InvokeRequest struct {
	GameID string `json:"gameId"`
	Step   int    `json:"step"`
}

type PlayerResponse struct {
	Tile              *int       `json:"tile,omitempty"`
	Board             *game.Board `json:"board,omitempty"`
	Error             string     `json:"error,omitempty"`
	LastAttemptedTile *int       `json:"lastAttemptedTile,omitempty"`
}

type CheckerResponse struct {
	Solved bool   `json:"solved"`
	GameID string `json:"gameId"`
	Step   int    `json:"step"`
	Error  string `json:"error,omitempty"`
}

// retry delays per the spec: 100ms / 200ms / 400ms.
var retryDelays = []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond}

// Invoke posts to /invoke with up to 3 retries on transport errors.
func (a *Agent) Invoke(ctx context.Context, req InvokeRequest, out any) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}

	var lastErr error
	for attempt := 0; attempt <= len(retryDelays); attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(retryDelays[attempt-1]):
			}
		}
		err = a.doOnce(ctx, body, out)
		if err == nil {
			return nil
		}
		lastErr = err
	}
	return fmt.Errorf("agent %s unreachable: %w", a.Name, lastErr)
}

func (a *Agent) doOnce(ctx context.Context, body []byte, out any) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.BaseURL+"/invoke", bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := a.HTTP.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 500 {
		return fmt.Errorf("agent %s returned %d: %s", a.Name, resp.StatusCode, string(respBody))
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("decode %s response: %w", a.Name, err)
	}
	return nil
}
