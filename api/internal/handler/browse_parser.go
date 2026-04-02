package handler

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"app/api/internal/model"
	"golang.org/x/net/proxy"
)

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

// dirEntry is one subdirectory found in a remote listing.
type dirEntry struct {
	Name string
	URL  string
}

// buildProxyClient returns an *http.Client configured to use the given proxy settings.
// If cfg is nil, disabled, or has no host, it returns a plain client with the given timeout.
func buildProxyClient(cfg *model.ProxyConfig, timeout time.Duration) *http.Client {
	if cfg == nil || !cfg.Enabled || cfg.Host == "" {
		return &http.Client{Timeout: timeout}
	}

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

	switch strings.ToUpper(cfg.Type) {
	case "SOCKS5":
		var auth *proxy.Auth
		if cfg.Username != "" {
			auth = &proxy.Auth{User: cfg.Username, Password: cfg.Password}
		}
		dialer, err := proxy.SOCKS5("tcp", addr, auth, proxy.Direct)
		if err != nil {
			break
		}
		if cd, ok := dialer.(proxy.ContextDialer); ok {
			return &http.Client{
				Transport: &http.Transport{DialContext: cd.DialContext},
				Timeout:   timeout,
			}
		}
	case "HTTP":
		var userInfo *url.Userinfo
		if cfg.Username != "" {
			userInfo = url.UserPassword(cfg.Username, cfg.Password)
		}
		proxyURL := &url.URL{
			Scheme: "http",
			Host:   addr,
			User:   userInfo,
		}
		return &http.Client{
			Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
			Timeout:   timeout,
		}
	}

	return &http.Client{Timeout: timeout}
}

// fetchURL GETs a URL using the given client and returns the response body as a string.
func fetchURL(client *http.Client, rawURL string) (string, error) {
	resp, err := client.Get(rawURL) //nolint:noctx
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

// findDirs parses directory listing HTML and returns a sorted slice of dirEntry
// for all entries that look like subdirectories (href ending with "/").
// Handles both relative hrefs and absolute URLs on the same host.
func findDirs(base *url.URL, body string) []dirEntry {
	seen := make(map[string]string)
	for _, m := range hrefRe.FindAllStringSubmatch(body, -1) {
		href := m[1]
		// skip anchors and query strings
		if strings.HasPrefix(href, "?") || strings.HasPrefix(href, "#") {
			continue
		}
		// skip parent / self
		if href == "../" || href == "./" {
			continue
		}

		// Parse the href and resolve against base
		ref, err := url.Parse(href)
		if err != nil {
			continue
		}
		abs := base.ResolveReference(ref)

		// For absolute URLs: only keep entries on the same host
		if ref.IsAbs() && abs.Host != base.Host {
			continue
		}

		// Must be a directory (path ends with "/")
		if !strings.HasSuffix(abs.Path, "/") {
			continue
		}
		// Must be a child of base, not the base itself or a parent
		if abs.Path == base.Path || !strings.HasPrefix(abs.Path, base.Path) {
			continue
		}

		// Use only the last path component as the directory name
		decoded, _ := url.PathUnescape(strings.TrimSuffix(abs.Path, "/"))
		name := path.Base(decoded)
		if name == "" || name == "." {
			name = abs.Path
		}
		seen[name] = abs.String()
	}

	entries := make([]dirEntry, 0, len(seen))
	for name, u := range seen {
		entries = append(entries, dirEntry{Name: name, URL: u})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries
}

// scanDir fetches a subdirectory URL and returns a RemoteMovie describing its contents.
func scanDir(client *http.Client, name, dirURL string) RemoteMovie {
	movie := RemoteMovie{
		Name:          name,
		URL:           dirURL,
		SubtitleFiles: []RemoteFile{},
	}

	body, err := fetchURL(client, dirURL)
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
		decoded, _ := url.PathUnescape(href)
		fname := path.Base(decoded)

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

// scanFlatDir parses a directory listing that contains video files directly
// (no per-movie subdirectories) and returns one RemoteMovie per video file.
func scanFlatDir(base *url.URL, body string) []RemoteMovie {
	var movies []RemoteMovie
	for _, m := range hrefRe.FindAllStringSubmatch(body, -1) {
		href := m[1]
		if strings.HasPrefix(href, "?") || strings.HasPrefix(href, "#") {
			continue
		}
		if href == "../" || href == "./" || strings.HasSuffix(href, "/") {
			continue
		}

		lower := strings.ToLower(href)
		ext := fileExt(lower)
		if !videoExts[ext] {
			continue
		}

		ref, err := url.Parse(href)
		if err != nil {
			continue
		}
		abs := base.ResolveReference(ref)
		// For absolute hrefs: only accept same-host files
		if ref.IsAbs() && abs.Host != base.Host {
			continue
		}

		decoded, _ := url.PathUnescape(href)
		fname := path.Base(decoded)
		size := extractSize(body, href)

		rf := RemoteFile{Name: fname, Size: size, URL: abs.String()}
		// Use filename without extension as the movie name
		name := strings.TrimSuffix(fname, path.Ext(fname))
		movies = append(movies, RemoteMovie{
			Name:          name,
			URL:           abs.String(),
			VideoFile:     &rf,
			SubtitleFiles: []RemoteFile{},
		})
	}
	if movies == nil {
		return []RemoteMovie{}
	}
	return movies
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
