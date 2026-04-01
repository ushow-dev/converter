package ffmpeg

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

// AudioStreamInfo holds metadata for a single audio stream found by ffprobe.
type AudioStreamInfo struct {
	Index    int    // absolute stream index in the file
	Language string // ISO 639-1/2 from tags, e.g. "eng", "rus"; empty if unset
	Title    string // free-form title tag, e.g. "DUB", "Original"
}

// ProbeAudioStreams returns metadata for every audio stream in the file.
func ProbeAudioStreams(ctx context.Context, inputPath string) ([]AudioStreamInfo, error) {
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-select_streams", "a",
		"-show_entries", "stream=index:stream_tags=language,title",
		"-of", "json",
		inputPath,
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe audio streams: %w", err)
	}

	var result struct {
		Streams []struct {
			Index int `json:"index"`
			Tags  struct {
				Language string `json:"language"`
				Title    string `json:"title"`
			} `json:"tags"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, fmt.Errorf("parse ffprobe audio output: %w", err)
	}

	streams := make([]AudioStreamInfo, len(result.Streams))
	for i, s := range result.Streams {
		streams[i] = AudioStreamInfo{
			Index:    s.Index,
			Language: s.Tags.Language,
			Title:    s.Tags.Title,
		}
	}
	return streams, nil
}
