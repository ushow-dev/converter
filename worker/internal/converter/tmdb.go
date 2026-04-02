package converter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"
)

type tmdbMetadata struct {
	Year         int
	Title        string
	BackdropPath string
	PosterPath   string
}

// fetchTMDBMetadata fetches movie details from TMDB and returns metadata including
// year, title, backdrop_path, and poster_path.
func fetchTMDBMetadata(ctx context.Context, apiKey, tmdbID string) (*tmdbMetadata, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	detailURL := fmt.Sprintf("https://api.themoviedb.org/3/movie/%s?api_key=%s", tmdbID, apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, detailURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("TMDB details returned HTTP %d", resp.StatusCode)
	}

	var details struct {
		Title        string `json:"title"`
		ReleaseDate  string `json:"release_date"`
		BackdropPath string `json:"backdrop_path"`
		PosterPath   string `json:"poster_path"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&details); err != nil {
		return nil, err
	}

	meta := &tmdbMetadata{
		Title:        details.Title,
		BackdropPath: details.BackdropPath,
		PosterPath:   details.PosterPath,
	}
	if len(details.ReleaseDate) >= 4 {
		if y, err := strconv.Atoi(details.ReleaseDate[:4]); err == nil {
			meta.Year = y
		}
	}
	return meta, nil
}

// fetchTMDBTVMetadata fetches TV series details from TMDB.
func fetchTMDBTVMetadata(ctx context.Context, apiKey, tmdbID string) (*tmdbMetadata, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	detailURL := fmt.Sprintf("https://api.themoviedb.org/3/tv/%s?api_key=%s", tmdbID, apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, detailURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("TMDB TV details returned HTTP %d", resp.StatusCode)
	}

	var details struct {
		Name         string `json:"name"`
		FirstAirDate string `json:"first_air_date"`
		BackdropPath string `json:"backdrop_path"`
		PosterPath   string `json:"poster_path"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&details); err != nil {
		return nil, err
	}

	meta := &tmdbMetadata{
		Title:        details.Name,
		BackdropPath: details.BackdropPath,
		PosterPath:   details.PosterPath,
	}
	if len(details.FirstAirDate) >= 4 {
		if y, err := strconv.Atoi(details.FirstAirDate[:4]); err == nil {
			meta.Year = y
		}
	}
	return meta, nil
}

// downloadImage downloads an image from url and saves it to destPath.
func downloadImage(ctx context.Context, url, destPath string) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("image download returned HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	return err
}
