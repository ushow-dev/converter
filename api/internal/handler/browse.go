package handler

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"app/api/internal/auth"
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

var (
	// matches <a href="..."> — capture group 1 is the href value
	hrefRe = regexp.MustCompile(`(?i)<a\s[^>]*href="([^"]+)"`)

	// strip HTML tags
	tagRe = regexp.MustCompile(`<[^>]+>`)

	// Apache/Nginx date-time column — several common formats:
	//   2024-01-15 10:30        (standard autoindex)
	//   2024-01-15 10:30:45     (with seconds)
	//   15-Jan-2024 10:30       (older Apache)
	apacheDateRe = regexp.MustCompile(
		`(?:\d{4}-\d{2}-\d{2}|\d{2}-\w{3}-\d{4})\s+\d{2}:\d{2}(?::\d{2})?`,
	)

	// Apache size token: first non-whitespace field after the date column.
	// Matches "1.4G", "512M", "44K", or a raw byte count.
	apacheSizeRe = regexp.MustCompile(`\s+(\d+(?:\.\d+)?[KMGT]?)\s`)

	videoExts = map[string]bool{
		".mkv": true, ".mp4": true, ".avi": true,
		".mov": true, ".m4v": true, ".ts": true, ".m2ts": true,
	}
)

// BrowseHandler handles remote directory listing requests.
type BrowseHandler struct {
	client *http.Client
}

// NewBrowseHandler creates a BrowseHandler with a 15-second timeout.
func NewBrowseHandler() *BrowseHandler {
	return &BrowseHandler{
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

// Browse handles GET /api/admin/remote-browse?url=...
// It fetches the given URL (expected: Apache/Nginx directory listing),
// discovers one level of subdirectories, and for each returns the
// video file and subtitle files found inside.
func (h *BrowseHandler) Browse(w http.ResponseWriter, r *http.Request) {
	cid := auth.GetCorrelationID(r.Context())
	rawURL := r.URL.Query().Get("url")
	if rawURL == "" {
		respondError(w, http.StatusBadRequest, "MISSING_URL", "url query parameter is required", false, cid)
		return
	}

	base, err := url.Parse(rawURL)
	if err != nil {
		respondError(w, http.StatusBadRequest, "INVALID_URL", "invalid url: "+err.Error(), false, cid)
		return
	}

	// Fetch root listing
	body, err := h.fetch(rawURL)
	if err != nil {
		respondError(w, http.StatusBadGateway, "FETCH_ERROR", "failed to fetch url: "+err.Error(), true, cid)
		return
	}

	// Find subdirectory links (href ending with "/", not "..", not absolute http(s))
	dirs := h.findDirs(base, body)
	if len(dirs) == 0 {
		respondJSON(w, http.StatusOK, []RemoteMovie{})
		return
	}

	// Concurrently scan each subdirectory — cap at 10 goroutines
	sem := make(chan struct{}, 10)
	var mu sync.Mutex
	movies := make([]RemoteMovie, 0, len(dirs))

	var wg sync.WaitGroup
	for name, dirURL := range dirs {
		wg.Add(1)
		go func(name, dirURL string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			movie := h.scanDir(name, dirURL)
			mu.Lock()
			movies = append(movies, movie)
			mu.Unlock()
		}(name, dirURL)
	}
	wg.Wait()

	respondJSON(w, http.StatusOK, movies)
}

// fetch GETs a URL and returns the response body as a string.
func (h *BrowseHandler) fetch(rawURL string) (string, error) {
	resp, err := h.client.Get(rawURL) //nolint:noctx
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20)) // 4 MB cap
	return string(b), err
}

// findDirs parses directory listing HTML and returns name→absoluteURL map
// for all entries that look like subdirectories (href ending with "/").
func (h *BrowseHandler) findDirs(base *url.URL, body string) map[string]string {
	dirs := make(map[string]string)
	for _, m := range hrefRe.FindAllStringSubmatch(body, -1) {
		href := m[1]
		// skip parent dir, anchors, query strings, and absolute URLs
		if strings.HasPrefix(href, "?") ||
			strings.HasPrefix(href, "#") ||
			strings.HasPrefix(href, "http://") ||
			strings.HasPrefix(href, "https://") ||
			href == "../" || href == "./" {
			continue
		}
		if !strings.HasSuffix(href, "/") {
			continue
		}
		// Resolve relative to base
		ref, err := url.Parse(href)
		if err != nil {
			continue
		}
		abs := base.ResolveReference(ref).String()
		// Use the unescaped directory name (trim trailing slash)
		name, _ := url.PathUnescape(strings.TrimSuffix(href, "/"))
		if name == "" {
			name = href
		}
		dirs[name] = abs
	}
	return dirs
}

// scanDir fetches a subdirectory URL and returns a RemoteMovie describing its contents.
func (h *BrowseHandler) scanDir(name, dirURL string) RemoteMovie {
	movie := RemoteMovie{
		Name:          name,
		URL:           dirURL,
		SubtitleFiles: []RemoteFile{},
	}

	body, err := h.fetch(dirURL)
	if err != nil {
		return movie
	}

	base, err := url.Parse(dirURL)
	if err != nil {
		return movie
	}

	for _, m := range hrefRe.FindAllStringSubmatch(body, -1) {
		href := m[1]
		if strings.HasPrefix(href, "?") ||
			strings.HasPrefix(href, "#") ||
			strings.HasPrefix(href, "http://") ||
			strings.HasPrefix(href, "https://") ||
			href == "../" || href == "./" ||
			strings.HasSuffix(href, "/") {
			continue
		}

		lower := strings.ToLower(href)
		ext := fileExt(lower)

		if !videoExts[ext] && ext != ".srt" {
			continue
		}

		ref, err := url.Parse(href)
		if err != nil {
			continue
		}
		abs := base.ResolveReference(ref).String()
		fname, _ := url.PathUnescape(href)

		// Try to extract size from the surrounding row in the HTML body.
		size := extractSize(body, href)

		rf := RemoteFile{Name: fname, Size: size, URL: abs}

		if ext == ".srt" {
			movie.SubtitleFiles = append(movie.SubtitleFiles, rf)
		} else if movie.VideoFile == nil {
			movie.VideoFile = &rf
		}
	}

	return movie
}

// fileExt returns the lowercase file extension for a given filename/href.
func fileExt(name string) string {
	idx := strings.LastIndex(name, ".")
	if idx < 0 {
		return ""
	}
	return name[idx:]
}

// extractSize finds the file size in an Apache-style directory listing row.
// Apache format (after tag stripping): "filename   YYYY-MM-DD HH:MM   <size>"
// We locate the date-time stamp first, then take the very next token — this
// avoids false positives from numbers inside the filename itself.
func extractSize(body, href string) string {
	idx := strings.Index(body, `"`+href+`"`)
	if idx < 0 {
		return ""
	}
	end := idx + 512
	if end > len(body) {
		end = len(body)
	}
	// Strip HTML tags from the row snippet
	plain := tagRe.ReplaceAllString(body[idx:end], " ")

	// Find the date-time column
	loc := apacheDateRe.FindStringIndex(plain)
	if loc == nil {
		return ""
	}
	afterDate := plain[loc[1]:]

	// The size token is the first field after the date-time
	m := apacheSizeRe.FindStringSubmatch(afterDate)
	if m == nil {
		return ""
	}
	return formatRemoteSize(m[1])
}

// formatRemoteSize normalises common Apache size representations.
// Apache uses suffixes: K, M, G (no decimal for small sizes).
func formatRemoteSize(s string) string {
	suffix := map[byte]string{
		'K': "KB", 'M': "MB", 'G': "GB", 'T': "TB",
	}
	if len(s) == 0 {
		return s
	}
	last := s[len(s)-1]
	if unit, ok := suffix[last]; ok {
		return s[:len(s)-1] + " " + unit
	}
	return s + " B"
}
