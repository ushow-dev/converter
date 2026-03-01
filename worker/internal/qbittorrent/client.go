package qbittorrent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"
)

// ErrAuth is returned when qBittorrent rejects login credentials.
var ErrAuth = errors.New("qBittorrent authentication failed")

// TorrentInfo holds the fields we need from /api/v2/torrents/info.
type TorrentInfo struct {
	Hash        string  `json:"hash"`
	Name        string  `json:"name"`
	State       string  `json:"state"`
	Progress    float64 `json:"progress"`
	Size        int64   `json:"size"`
	ContentPath string  `json:"content_path"` // path to file or folder
	SavePath    string  `json:"save_path"`
}

// IsComplete reports whether the torrent has finished downloading.
func (t TorrentInfo) IsComplete() bool {
	return t.Progress >= 1.0
}

// IsError reports a terminal error state in qBittorrent.
func (t TorrentInfo) IsError() bool {
	return t.State == "error" || t.State == "missingFiles"
}

// Client talks to the qBittorrent Web API v2.
type Client struct {
	baseURL  string
	user     string
	pass     string
	http     *http.Client
	loggedIn bool
}

// New creates a qBittorrent Client.
func New(baseURL, user, pass string) *Client {
	jar, _ := cookiejar.New(nil)
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		user:    user,
		pass:    pass,
		http:    &http.Client{Timeout: 15 * time.Second, Jar: jar},
	}
}

// Login authenticates and stores the session cookie.
func (c *Client) Login(ctx context.Context) error {
	body := url.Values{"username": {c.user}, "password": {c.pass}}
	resp, err := c.post(ctx, "/api/v2/auth/login", body)
	if err != nil {
		return fmt.Errorf("login request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if strings.TrimSpace(string(raw)) != "Ok." {
		return ErrAuth
	}
	c.loggedIn = true
	return nil
}

// EnsureLoggedIn re-authenticates if session has expired (best-effort).
func (c *Client) EnsureLoggedIn(ctx context.Context) error {
	if !c.loggedIn {
		return c.Login(ctx)
	}
	return nil
}

// AddTorrent adds a magnet/URL torrent and returns the infohash extracted from
// the magnet URI (lowercase hex). savePath is where qBittorrent should save
// the downloaded files.
func (c *Client) AddTorrent(ctx context.Context, magnetURL, savePath string) (string, error) {
	if err := c.EnsureLoggedIn(ctx); err != nil {
		return "", err
	}
	hash, err := extractInfohash(magnetURL)
	if err != nil {
		return "", fmt.Errorf("extract infohash: %w", err)
	}

	body := url.Values{
		"urls":     {magnetURL},
		"savepath": {savePath},
		"category": {"media"},
	}
	resp, err := c.post(ctx, "/api/v2/torrents/add", body)
	if err != nil {
		return "", fmt.Errorf("add torrent: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	result := strings.TrimSpace(string(raw))
	if result != "Ok." && result != "Duplicate torrent!" {
		return "", fmt.Errorf("unexpected response: %q", result)
	}
	return hash, nil
}

// GetTorrentInfo fetches the current state of a torrent by hash.
// Returns nil, nil when the torrent is not yet tracked by qBittorrent.
func (c *Client) GetTorrentInfo(ctx context.Context, hash string) (*TorrentInfo, error) {
	if err := c.EnsureLoggedIn(ctx); err != nil {
		return nil, err
	}
	params := url.Values{"hashes": {hash}, "limit": {"1"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/api/v2/torrents/info?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get torrent info: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		c.loggedIn = false
		return nil, fmt.Errorf("session expired")
	}
	var infos []TorrentInfo
	if err := json.NewDecoder(resp.Body).Decode(&infos); err != nil {
		return nil, fmt.Errorf("decode torrent info: %w", err)
	}
	if len(infos) == 0 {
		return nil, nil
	}
	return &infos[0], nil
}

// WaitForDownload polls until the torrent completes or ctx is cancelled.
// progressFn is called on each poll with the current progress (0–100).
func (c *Client) WaitForDownload(
	ctx context.Context, hash string,
	progressFn func(int),
) (*TorrentInfo, error) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	var lastProgress int
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			info, err := c.GetTorrentInfo(ctx, hash)
			if err != nil {
				slog.Warn("qbittorrent poll error", "hash", hash, "error", err)
				continue
			}
			if info == nil {
				slog.Warn("torrent not yet visible in qbittorrent", "hash", hash)
				continue
			}
			if info.IsError() {
				return nil, fmt.Errorf("torrent error state: %s", info.State)
			}
			pct := int(info.Progress * 100)
			if pct != lastProgress {
				lastProgress = pct
				progressFn(pct)
			}
			if info.IsComplete() {
				return info, nil
			}
		}
	}
}

// DeleteTorrent removes a torrent from qBittorrent (does not delete files).
func (c *Client) DeleteTorrent(ctx context.Context, hash string) error {
	body := url.Values{"hashes": {hash}, "deleteFiles": {"false"}}
	resp, err := c.post(ctx, "/api/v2/torrents/delete", body)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func (c *Client) post(ctx context.Context, path string, vals url.Values) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+path, strings.NewReader(vals.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusForbidden {
		c.loggedIn = false
		resp.Body.Close()
		return nil, fmt.Errorf("qbittorrent session expired (403)")
	}
	return resp, nil
}

// extractInfohash parses the infohash from a magnet URI.
// Handles both 40-char hex and 32-char base32 encodings.
func extractInfohash(magnetURL string) (string, error) {
	u, err := url.Parse(magnetURL)
	if err != nil {
		return "", fmt.Errorf("parse magnet URL: %w", err)
	}
	xt := u.Query().Get("xt")
	const prefix = "urn:btih:"
	if !strings.HasPrefix(xt, prefix) {
		return "", fmt.Errorf("no btih in magnet xt: %q", xt)
	}
	hash := strings.ToLower(strings.TrimPrefix(xt, prefix))
	return hash, nil
}
