package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// IncomingItem — fields the worker needs from the API response
type IncomingItem struct {
	ID             int64   `json:"id"`
	SourcePath     string  `json:"source_path"`
	SourceFilename string  `json:"source_filename"`
	ContentKind    string  `json:"content_kind"`
	NormalizedName *string `json:"normalized_name,omitempty"`
	TMDBID         *string `json:"tmdb_id,omitempty"`
}

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

func NewClient(baseURL, token string) *Client {
	return &Client{
		baseURL: baseURL,
		token:   token,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// Claim claims up to limit items from the API.
func (c *Client) Claim(ctx context.Context, claimTTLSec int) ([]IncomingItem, error) {
	body, _ := json.Marshal(map[string]int{"limit": 1, "claim_ttl_sec": claimTTLSec})
	var resp struct {
		Items []IncomingItem `json:"items"`
	}
	if err := c.post(ctx, "/api/ingest/incoming/claim", body, &resp); err != nil {
		return nil, err
	}
	return resp.Items, nil
}

// Progress reports progress for an item.
func (c *Client) Progress(ctx context.Context, id int64, status string) error {
	body, _ := json.Marshal(map[string]any{"id": id, "status": status, "progress_percent": 0})
	return c.post(ctx, "/api/ingest/incoming/progress", body, nil)
}

// Fail reports a failure for an item.
func (c *Client) Fail(ctx context.Context, id int64, msg string) error {
	body, _ := json.Marshal(map[string]any{"id": id, "error_message": msg})
	return c.post(ctx, "/api/ingest/incoming/fail", body, nil)
}

// Complete marks an item as completed and returns the job_id.
func (c *Client) Complete(ctx context.Context, id int64, localPath string) (string, error) {
	body, _ := json.Marshal(map[string]any{"id": id, "local_path": localPath})
	var resp struct {
		JobID string `json:"job_id"`
	}
	if err := c.post(ctx, "/api/ingest/incoming/complete", body, &resp); err != nil {
		return "", err
	}
	return resp.JobID, nil
}

func (c *Client) post(ctx context.Context, path string, body []byte, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request %s: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Service-Token", c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("POST %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("POST %s: status %d", path, resp.StatusCode)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}
