package indexer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/sony/gobreaker"

	"app/api/internal/model"
)

// ErrIndexerUnavailable is returned when the circuit breaker is open or all
// retries are exhausted.
var ErrIndexerUnavailable = errors.New("indexer unavailable")

// ProwlarrClient implements Provider against a Prowlarr instance.
type ProwlarrClient struct {
	baseURL string
	apiKey  string
	http    *http.Client
	cb      *gobreaker.CircuitBreaker
}

// prowlarrRelease is the raw JSON structure returned by Prowlarr /api/v1/search.
type prowlarrRelease struct {
	GUID        string `json:"guid"`
	IndexerID   int    `json:"indexerId"`
	Indexer     string `json:"indexer"`
	Title       string `json:"title"`
	Size        int64  `json:"size"`
	Seeders     int    `json:"seeders"`
	Leechers    int    `json:"leechers"`
	DownloadURL string `json:"downloadUrl"`
	MagnetURL   string `json:"magnetUrl"`
}

// NewProwlarrClient creates a ProwlarrClient with resilience defaults:
//   - HTTP timeout: 10 s
//   - Circuit breaker: open after 5 failures in 60 s, half-open after 30 s
func NewProwlarrClient(baseURL, apiKey string) *ProwlarrClient {
	cbSettings := gobreaker.Settings{
		Name:        "prowlarr",
		MaxRequests: 1,
		Interval:    60 * time.Second,
		Timeout:     30 * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= 5
		},
		OnStateChange: func(name string, from, to gobreaker.State) {
			slog.Warn("circuit breaker state change",
				"breaker", name, "from", from.String(), "to", to.String())
		},
	}
	return &ProwlarrClient{
		baseURL: baseURL,
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 10 * time.Second},
		cb:      gobreaker.NewCircuitBreaker(cbSettings),
	}
}

// Search implements Provider.
// Retries up to 3 times with exponential backoff (500 ms → 1 s → 2 s).
// The whole retry sequence is wrapped by the circuit breaker.
func (c *ProwlarrClient) Search(
	ctx context.Context, query, contentType string, limit int,
) ([]model.SearchResult, error) {
	result, err := c.cb.Execute(func() (interface{}, error) {
		return c.searchWithRetry(ctx, query, contentType, limit)
	})
	if err != nil {
		if errors.Is(err, gobreaker.ErrOpenState) ||
			errors.Is(err, gobreaker.ErrTooManyRequests) {
			return nil, ErrIndexerUnavailable
		}
		return nil, ErrIndexerUnavailable
	}
	releases, _ := result.([]model.SearchResult)
	return releases, nil
}

func (c *ProwlarrClient) searchWithRetry(
	ctx context.Context, query, contentType string, limit int,
) ([]model.SearchResult, error) {
	delays := []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second}
	var lastErr error
	for attempt := 0; attempt <= len(delays); attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delays[attempt-1]):
			}
		}
		results, err := c.doSearch(ctx, query, contentType, limit)
		if err == nil {
			return results, nil
		}
		lastErr = err
		slog.Warn("prowlarr search attempt failed",
			"attempt", attempt+1, "error", err)
	}
	return nil, fmt.Errorf("all retries exhausted: %w", lastErr)
}

func (c *ProwlarrClient) doSearch(
	ctx context.Context, query, contentType string, limit int,
) ([]model.SearchResult, error) {
	params := url.Values{
		"query":  {query},
		"type":   {"search"},
		"limit":  {strconv.Itoa(limit)},
		"apikey": {c.apiKey},
	}
	// Map content_type to Prowlarr categories (movies → 2000).
	if contentType == "movie" {
		params.Add("categories[]", "2000")
	}

	endpoint := c.baseURL + "/api/v1/search?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("X-Api-Key", c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http do: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("prowlarr returned status %d", resp.StatusCode)
	}

	var releases []prowlarrRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return normalise(releases, contentType), nil
}

// normalise converts raw Prowlarr releases to the internal SearchResult model.
func normalise(releases []prowlarrRelease, contentType string) []model.SearchResult {
	out := make([]model.SearchResult, 0, len(releases))
	for _, r := range releases {
		sourceRef := r.MagnetURL
		if sourceRef == "" {
			sourceRef = r.DownloadURL
		}
		out = append(out, model.SearchResult{
			ExternalID:  fmt.Sprintf("prowlarr:%d:%s", r.IndexerID, r.GUID),
			Title:       r.Title,
			SourceType:  "torrent",
			SourceRef:   sourceRef,
			SizeBytes:   r.Size,
			Seeders:     r.Seeders,
			Leechers:    r.Leechers,
			Indexer:     r.Indexer,
			ContentType: contentType,
			CreatedAt:   time.Now().UTC(),
		})
	}
	return out
}
