package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// IncomingItem — fields the worker needs from the scanner API response.
type IncomingItem struct {
	ID             int64   `json:"id"`
	SourcePath     string  `json:"source_path"`
	SourceFilename string  `json:"source_filename"`
	ContentKind    string  `json:"content_kind"`
	NormalizedName *string `json:"normalized_name,omitempty"`
	TMDBID         *string `json:"tmdb_id,omitempty"`
}

// Client is an HTTP client for the scanner ingest API.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewClient creates a Client that talks to the scanner API at baseURL.
func NewClient(baseURL, token string) *Client {
	return &Client{
		baseURL: baseURL,
		token:   token,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// Claim claims up to 1 item from the scanner API.
func (c *Client) Claim(ctx context.Context, claimTTLSec int) ([]IncomingItem, error) {
	body, _ := json.Marshal(map[string]int{"limit": 1, "claim_ttl_sec": claimTTLSec})
	var resp struct {
		Items []IncomingItem `json:"items"`
	}
	if err := c.post(ctx, "/api/v1/incoming/claim", body, &resp); err != nil {
		return nil, err
	}
	return resp.Items, nil
}

// Progress reports copying progress for an item.
func (c *Client) Progress(ctx context.Context, id int64, status string) error {
	body, _ := json.Marshal(map[string]string{"status": status})
	return c.post(ctx, fmt.Sprintf("/api/v1/incoming/%d/progress", id), body, nil)
}

// Fail reports a failure for an item.
func (c *Client) Fail(ctx context.Context, id int64, msg string) error {
	body, _ := json.Marshal(map[string]string{"error_message": msg})
	return c.post(ctx, fmt.Sprintf("/api/v1/incoming/%d/fail", id), body, nil)
}

// Complete marks an item as completed in the scanner.
func (c *Client) Complete(ctx context.Context, id int64) error {
	return c.post(ctx, fmt.Sprintf("/api/v1/incoming/%d/complete", id), []byte("{}"), nil)
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
