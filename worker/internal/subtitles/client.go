package subtitles

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const baseURL = "https://api.opensubtitles.com/api/v1"

// Result holds the metadata for one subtitle file returned by Search.
type Result struct {
	FileID   int
	Language string
}

// Client calls the OpenSubtitles.com REST API v1.
type Client struct {
	apiKey     string
	httpClient *http.Client
}

// NewClient creates a Client with the given API key.
func NewClient(apiKey string) *Client {
	return &Client{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

// Search searches for subtitles by TMDB ID for the given languages.
// It returns at most one result per language (highest download count).
func (c *Client) Search(ctx context.Context, tmdbID string, languages []string) ([]Result, error) {
	params := url.Values{}
	params.Set("tmdb_id", tmdbID)
	for _, lang := range languages {
		params.Add("languages", lang)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		baseURL+"/subtitles?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("opensubtitles search: status %d: %s", resp.StatusCode, body)
	}

	var payload struct {
		Data []struct {
			Attributes struct {
				Language      string `json:"language"`
				DownloadCount int    `json:"download_count"`
				Files         []struct {
					FileID int `json:"file_id"`
				} `json:"files"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("opensubtitles search decode: %w", err)
	}

	// Pick best (highest download count) per language.
	best := map[string]struct {
		fileID    int
		downloads int
	}{}
	for _, item := range payload.Data {
		lang := item.Attributes.Language
		if len(item.Attributes.Files) == 0 {
			continue
		}
		fileID := item.Attributes.Files[0].FileID
		dl := item.Attributes.DownloadCount
		if prev, ok := best[lang]; !ok || dl > prev.downloads {
			best[lang] = struct {
				fileID    int
				downloads int
			}{fileID, dl}
		}
	}

	results := make([]Result, 0, len(best))
	for lang, b := range best {
		results = append(results, Result{FileID: b.fileID, Language: lang})
	}
	return results, nil
}

// DownloadURL requests a one-time download link for the given file ID.
func (c *Client) DownloadURL(ctx context.Context, fileID int) (string, error) {
	body, _ := json.Marshal(map[string]int{"file_id": fileID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		baseURL+"/download", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	c.setHeaders(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("opensubtitles download: status %d: %s", resp.StatusCode, b)
	}

	var payload struct {
		Link string `json:"link"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("opensubtitles download decode: %w", err)
	}
	if payload.Link == "" {
		return "", fmt.Errorf("opensubtitles download: empty link")
	}
	return payload.Link, nil
}

// FetchRaw downloads the subtitle file at the given URL and returns its bytes.
func (c *Client) FetchRaw(ctx context.Context, downloadURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch subtitle: status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Api-Key", c.apiKey)
	req.Header.Set("User-Agent", "MediaConverter/1.0")
	req.Header.Set("Accept", "application/json")
}
