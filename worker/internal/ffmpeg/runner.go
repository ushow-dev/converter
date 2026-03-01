package ffmpeg

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// Profile maps a profile name to ffmpeg output flags.
var profiles = map[string][]string{
	"mp4_h264_aac_1080p": {
		"-c:v", "libx264",
		"-preset", "medium",
		"-crf", "23",
		"-vf", "scale=1920:1080:force_original_aspect_ratio=decrease",
		"-c:a", "aac",
		"-b:a", "128k",
		"-movflags", "+faststart",
	},
}

// ProfileCodecs returns the video and audio codec names for a given profile.
func ProfileCodecs(profile string) (video, audio string) {
	switch profile {
	case "mp4_h264_aac_1080p":
		return "h264", "aac"
	default:
		return "unknown", "unknown"
	}
}

// Result holds the outcome of a successful ffmpeg run.
type Result struct {
	DurationSec int // total output duration in seconds
	VideoCodec  string
	AudioCodec  string
}

// Run executes ffmpeg to convert inputPath → outputPath using the named profile.
// progressFn is called periodically with a percentage (0–100).
// The context can be used to cancel the process.
func Run(
	ctx context.Context,
	inputPath, outputPath, profile string,
	progressFn func(int),
) (*Result, error) {
	flags, ok := profiles[profile]
	if !ok {
		return nil, fmt.Errorf("unknown ffmpeg profile %q", profile)
	}

	args := []string{"-hide_banner", "-y", "-i", inputPath}
	args = append(args, flags...)
	args = append(args, outputPath)

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	// ffmpeg writes progress to stderr.
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start ffmpeg: %w", err)
	}

	var totalSec float64
	progressParser := newProgressParser(func(current, total float64) {
		if total > 0 {
			pct := int(current / total * 100)
			progressFn(clamp(pct, 0, 100))
		}
	})

	scanner := bufio.NewScanner(stderr)
	scanner.Split(splitOnCarriageReturn)
	for scanner.Scan() {
		line := scanner.Text()
		if d := parseDuration(line); d > 0 {
			totalSec = d
		}
		progressParser.feed(line, totalSec)
		slog.Debug("ffmpeg", "line", line)
	}

	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("ffmpeg exited with error: %w", err)
	}

	videoCodec, audioCodec := ProfileCodecs(profile)
	return &Result{
		DurationSec: int(totalSec),
		VideoCodec:  videoCodec,
		AudioCodec:  audioCodec,
	}, nil
}

// ─── progress parsing ─────────────────────────────────────────────────────────

var (
	reDuration = regexp.MustCompile(`Duration:\s+(\d{2}):(\d{2}):(\d{2})\.(\d{2})`)
	reTime     = regexp.MustCompile(`time=(\d{2}):(\d{2}):(\d{2})\.(\d{2})`)
)

type progressParser struct {
	fn func(current, total float64)
}

func newProgressParser(fn func(current, total float64)) *progressParser {
	return &progressParser{fn: fn}
}

func (p *progressParser) feed(line string, totalSec float64) {
	if m := reTime.FindStringSubmatch(line); m != nil {
		current := parseHHMMSScs(m[1], m[2], m[3], m[4])
		p.fn(current, totalSec)
	}
}

func parseDuration(line string) float64 {
	m := reDuration.FindStringSubmatch(line)
	if m == nil {
		return 0
	}
	return parseHHMMSScs(m[1], m[2], m[3], m[4])
}

func parseHHMMSScs(h, m, s, cs string) float64 {
	hours, _ := strconv.Atoi(h)
	minutes, _ := strconv.Atoi(m)
	seconds, _ := strconv.Atoi(s)
	centisecs, _ := strconv.Atoi(cs)
	return float64(hours*3600+minutes*60+seconds) + float64(centisecs)/100
}

// splitOnCarriageReturn splits ffmpeg progress lines that use \r instead of \n.
func splitOnCarriageReturn(data []byte, atEOF bool) (advance int, token []byte, err error) {
	for i := 0; i < len(data); i++ {
		if data[i] == '\r' || data[i] == '\n' {
			return i + 1, data[:i], nil
		}
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Installed checks whether ffmpeg is available in PATH.
func Installed() bool {
	_, err := exec.LookPath("ffmpeg")
	return err == nil
}

// ProbeInfo runs ffprobe to extract duration from a media file.
// Returns duration in seconds, or 0 on error.
func ProbeInfo(ctx context.Context, filePath string) int {
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		filePath,
	)
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	trimmed := strings.TrimSpace(string(out))
	f, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return 0
	}
	return int(f)
}

