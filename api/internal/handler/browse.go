package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"sync"
	"time"

	"app/api/internal/auth"
	"app/api/internal/model"
)

const (
	browseLimitDefault = 100
	browseLimitMax     = 100
	browseOpTimeout    = 25 * time.Second // safety timeout per page
)

// RemoteFile describes a single file found in a remote directory listing.
type RemoteFile struct {
	Name string `json:"name"`
	Size string `json:"size"`
	URL  string `json:"url"`
}

// RemoteMovie represents a subdirectory that contains a video file.
type RemoteMovie struct {
	Name          string       `json:"name"`
	URL           string       `json:"url"`
	VideoFile     *RemoteFile  `json:"video_file"`
	SubtitleFiles []RemoteFile `json:"subtitle_files"`
}

// BrowseResponse is the JSON envelope returned by Browse.
type BrowseResponse struct {
	Items   []RemoteMovie `json:"items"`
	Total   int           `json:"total"`
	HasMore bool          `json:"has_more"`
}

// BrowseHandler handles remote directory listing requests.
type BrowseHandler struct{}

// NewBrowseHandler creates a BrowseHandler.
func NewBrowseHandler() *BrowseHandler {
	return &BrowseHandler{}
}

// Browse handles POST /api/admin/remote-browse.
// Body: {"url": "...", "proxy_config": {...} | null}
// It fetches the given URL (expected: Apache/Nginx directory listing),
// discovers one level of subdirectories, and for each returns the
// video file and subtitle files found inside.
func (h *BrowseHandler) Browse(w http.ResponseWriter, r *http.Request) {
	cid := auth.GetCorrelationID(r.Context())

	var req struct {
		URL         string             `json:"url"`
		ProxyConfig *model.ProxyConfig `json:"proxy_config"`
		Offset      int                `json:"offset"`
		Limit       int                `json:"limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "VALIDATION_ERROR", "invalid JSON body", false, cid)
		return
	}
	if req.URL == "" {
		respondError(w, http.StatusBadRequest, "MISSING_URL", "url is required", false, cid)
		return
	}
	if req.Limit <= 0 || req.Limit > browseLimitMax {
		req.Limit = browseLimitDefault
	}
	if req.Offset < 0 {
		req.Offset = 0
	}

	base, err := url.Parse(req.URL)
	if err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_URL", "invalid url: "+err.Error(), false, cid)
		return
	}

	client := buildProxyClient(req.ProxyConfig, 30*time.Second)

	// Fetch root listing
	body, err := fetchURL(client, req.URL)
	if err != nil {
		respondError(w, http.StatusBadGateway, "FETCH_ERROR", "failed to fetch url: "+err.Error(), true, cid)
		return
	}

	// Find subdirectory links (href ending with "/", not "..", not absolute http(s))
	allDirs := findDirs(base, body)
	if len(allDirs) == 0 {
		// Fallback: no subdirectories — treat each video file in the root as its own movie.
		movies := scanFlatDir(base, body)
		respondJSON(w, http.StatusOK, BrowseResponse{Items: movies, Total: len(movies), HasMore: false})
		return
	}

	// Paginate the sorted directory list.
	total := len(allDirs)
	end := req.Offset + req.Limit
	if end > total {
		end = total
	}
	var page []dirEntry
	if req.Offset < total {
		page = allDirs[req.Offset:end]
	}

	// Wrap the scan in a deadline so the HTTP handler always returns
	// within browseOpTimeout regardless of remote latency.
	opCtx, cancel := context.WithTimeout(r.Context(), browseOpTimeout)
	defer cancel()

	// Concurrently scan each subdirectory in this page — cap at 10 goroutines.
	sem := make(chan struct{}, 10)
	var mu sync.Mutex
	movies := make([]RemoteMovie, 0, len(page))

	var wg sync.WaitGroup
	for _, entry := range page {
		wg.Add(1)
		go func(name, dirURL string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-opCtx.Done():
				return
			}
			defer func() { <-sem }()

			movie := scanDir(client, name, dirURL)
			mu.Lock()
			movies = append(movies, movie)
			mu.Unlock()
		}(entry.Name, entry.URL)
	}
	wg.Wait()

	// Re-sort results (goroutines finish in arbitrary order).
	sort.Slice(movies, func(i, j int) bool { return movies[i].Name < movies[j].Name })

	respondJSON(w, http.StatusOK, BrowseResponse{
		Items:   movies,
		Total:   total,
		HasMore: end < total,
	})
}
