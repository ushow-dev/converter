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
	// uiHost is sent as the HTTP Host header on every API request.
	// qBittorrent 4.6+ validates the Host header and only accepts
	// "localhost" and "127.0.0.1" by default; using the Docker service
	// name (e.g. "qbittorrent:8080") causes 403 on all endpoints except
	// /api/v2/auth/login. Overriding Host to "localhost:<port>" while
	// keeping the TCP connection to the real hostname is the standard fix.
	uiHost string
}

// New creates a qBittorrent Client.
func New(baseURL, user, pass string) *Client {
	jar, _ := cookiejar.New(nil)
	u, _ := url.Parse(strings.TrimRight(baseURL, "/"))
	uiHost := "localhost"
	if p := u.Port(); p != "" {
		uiHost = "localhost:" + p
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		user:    user,
		pass:    pass,
		http:    &http.Client{Timeout: 15 * time.Second, Jar: jar},
		uiHost:  uiHost,
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

// AddTorrent adds a magnet URI or .torrent URL and returns the infohash
// (lowercase hex). savePath is where qBittorrent should save the files.
//
// For magnet URIs the hash is extracted from the URI directly.
// For .torrent URLs the hash is discovered by diffing the torrent list
// before and after the add call (qBittorrent fetches the file itself).
func (c *Client) AddTorrent(ctx context.Context, sourceRef, savePath string) (string, error) {
	if err := c.EnsureLoggedIn(ctx); err != nil {
		return "", err
	}
	if strings.HasPrefix(sourceRef, "magnet:") {
		return c.addMagnet(ctx, sourceRef, savePath)
	}
	return c.addTorrentURL(ctx, sourceRef, savePath)
}

func (c *Client) addMagnet(ctx context.Context, magnetURL, savePath string) (string, error) {
	hash, err := extractInfohash(magnetURL)
	if err != nil {
		return "", fmt.Errorf("extract infohash: %w", err)
	}
	if err := c.postURLs(ctx, magnetURL, savePath); err != nil {
		return "", err
	}
	return hash, nil
}

// addTorrentURL adds a .torrent file URL to qBittorrent and discovers the
// resulting infohash by comparing the torrent list before and after.
func (c *Client) addTorrentURL(ctx context.Context, torrentURL, savePath string) (string, error) {
	before, err := c.listHashes(ctx)
	if err != nil {
		// Session may have expired; force re-login and retry once.
		c.loggedIn = false
		if loginErr := c.EnsureLoggedIn(ctx); loginErr != nil {
			return "", fmt.Errorf("list hashes before add (re-login failed: %v): %w", loginErr, err)
		}
		before, err = c.listHashes(ctx)
		if err != nil {
			return "", fmt.Errorf("list hashes before add: %w", err)
		}
	}
	if err := c.postURLs(ctx, torrentURL, savePath); err != nil {
		return "", err
	}
	// qBittorrent fetches the .torrent asynchronously; poll until new hash appears.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(2 * time.Second)
		after, err := c.listHashes(ctx)
		if err != nil {
			continue
		}
		for h := range after {
			if !before[h] {
				return h, nil
			}
		}
	}
	return "", fmt.Errorf("timeout waiting for torrent hash after adding %q", torrentURL)
}

func (c *Client) postURLs(ctx context.Context, ref, savePath string) error {
	body := url.Values{
		"urls":     {ref},
		"savepath": {savePath},
		"category": {"media"},
	}
	resp, err := c.post(ctx, "/api/v2/torrents/add", body)
	if err != nil {
		return fmt.Errorf("add torrent: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	result := strings.TrimSpace(string(raw))
	if result != "Ok." && result != "Duplicate torrent!" {
		return fmt.Errorf("unexpected qbittorrent response: %q", result)
	}
	return nil
}

// listHashes returns all current torrent hashes known to qBittorrent.
func (c *Client) listHashes(ctx context.Context) (map[string]bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/api/v2/torrents/info", nil)
	if err != nil {
		return nil, err
	}
	req.Host = c.uiHost
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list torrents: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden {
		c.loggedIn = false
		return nil, fmt.Errorf("host not allowed or session expired (403)")
	}
	var infos []TorrentInfo
	if err := json.NewDecoder(resp.Body).Decode(&infos); err != nil {
		return nil, fmt.Errorf("decode torrent list: %w", err)
	}
	hashes := make(map[string]bool, len(infos))
	for _, t := range infos {
		hashes[t.Hash] = true
	}
	return hashes, nil
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
	req.Host = c.uiHost
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
	req.Host = c.uiHost
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
