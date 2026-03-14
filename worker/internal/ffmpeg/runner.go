package ffmpeg

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// HLSResult holds the outcome of a successful HLS conversion.
type HLSResult struct {
	DurationSec int
	HasAudio    bool
}

// RunHLS converts inputPath to a 3-variant HLS stream (720p / 480p / 360p)
// and writes the following layout to outputDir:
//
//	outputDir/
//	  master.m3u8
//	  720/index.m3u8  720/seg000.ts …
//	  480/index.m3u8  480/seg000.ts …
//	  360/index.m3u8  360/seg000.ts …
//
// segDur is the HLS segment duration in seconds (4 is recommended).
// progressFn is called periodically with 0–99 (100 is set by the caller on success).
func RunHLS(
	ctx context.Context,
	inputPath, outputDir string,
	segDur int,
	progressFn func(int),
) (*HLSResult, error) {
	// Create variant subdirs — ffmpeg requires them to exist.
	for _, sub := range []string{"720", "480", "360"} {
		dir := filepath.Join(outputDir, sub)
		if err := os.MkdirAll(dir, 0o777); err != nil {
			return nil, fmt.Errorf("create HLS subdir %s: %w", sub, err)
		}
		_ = os.Chmod(dir, 0o777)
	}

	// Detect source properties.
	fps, err := probeFPS(ctx, inputPath)
	if err != nil || fps <= 0 {
		fps = 25.0
		slog.Warn("could not probe source FPS, falling back to 25", "input", inputPath)
	}
	targetFPS := snapFPS(fps)
	gop := int(math.Round(float64(segDur) * targetFPS))
	if gop < 1 {
		gop = 1
	}
	hasAudio := probeHasAudio(ctx, inputPath)

	segS := strconv.Itoa(segDur)
	gopS := strconv.Itoa(gop)

	slog.Info("HLS encode params",
		"target_fps", targetFPS, "gop", gop,
		"seg_dur", segDur, "has_audio", hasAudio)

	filterComplex := "[0:v]split=3[v720][v480][v360];" +
		"[v720]scale=-2:720:flags=bicubic[v720o];" +
		"[v480]scale=-2:480:flags=bicubic[v480o];" +
		"[v360]scale=-2:360:flags=bicubic[v360o]"

	// audio source index: 0 = original file audio; 1 = synthetic silence.
	aSrc := "0"
	var args []string
	args = append(args, "-hide_banner", "-y", "-i", inputPath)
	if !hasAudio {
		args = append(args,
			"-f", "lavfi", "-i", "anullsrc=channel_layout=stereo:sample_rate=48000")
		aSrc = "1"
	}

	args = append(args,
		"-map_metadata", "-1",
		"-map_chapters", "-1",
		"-filter_complex", filterComplex,
	)

	// 720p stream — per-stream map keeps HLS muxer happy (shared audio causes exit 234).
	// bufsize ~2× maxrate gives the encoder headroom for variable scenes.
	args = append(args,
		"-map", "[v720o]", "-map", aSrc+":a:0",
		"-c:v:0", "libx264", "-preset", "faster",
		"-profile:v:0", "high", "-level:v:0", "4.0",
		"-pix_fmt:v:0", "yuv420p", "-sc_threshold:v:0", "0",
		"-x264-params:v:0", "rc-lookahead=10",
		"-b:v:0", "1050k", "-maxrate:v:0", "1155k", "-bufsize:v:0", "2300k",
		"-g:v:0", gopS, "-keyint_min:v:0", gopS,
		"-c:a:0", "aac", "-b:a:0", "80k", "-ar:a:0", "48000", "-ac:a:0", "2",
	)

	// 480p stream
	args = append(args,
		"-map", "[v480o]", "-map", aSrc+":a:0",
		"-c:v:1", "libx264", "-preset", "faster",
		"-profile:v:1", "high", "-level:v:1", "4.0",
		"-pix_fmt:v:1", "yuv420p", "-sc_threshold:v:1", "0",
		"-x264-params:v:1", "rc-lookahead=10",
		"-b:v:1", "700k", "-maxrate:v:1", "770k", "-bufsize:v:1", "1500k",
		"-g:v:1", gopS, "-keyint_min:v:1", gopS,
		"-c:a:1", "aac", "-b:a:1", "80k", "-ar:a:1", "48000", "-ac:a:1", "2",
	)

	// 360p stream
	args = append(args,
		"-map", "[v360o]", "-map", aSrc+":a:0",
		"-c:v:2", "libx264", "-preset", "faster",
		"-profile:v:2", "high", "-level:v:2", "4.0",
		"-pix_fmt:v:2", "yuv420p", "-sc_threshold:v:2", "0",
		"-x264-params:v:2", "rc-lookahead=10",
		"-b:v:2", "320k", "-maxrate:v:2", "352k", "-bufsize:v:2", "700k",
		"-g:v:2", gopS, "-keyint_min:v:2", gopS,
		"-c:a:2", "aac", "-b:a:2", "80k", "-ar:a:2", "48000", "-ac:a:2", "2",
	)

	if !hasAudio {
		args = append(args, "-shortest")
	}

	args = append(args,
		"-f", "hls",
		"-hls_time", segS,
		"-hls_playlist_type", "vod",
		"-hls_list_size", "0",
		"-hls_flags", "independent_segments",
		"-master_pl_name", "master.m3u8",
		"-var_stream_map", "v:0,a:0,name:720 v:1,a:1,name:480 v:2,a:2,name:360",
		"-hls_segment_filename", filepath.Join(outputDir, "%v", "seg%03d.ts"),
		filepath.Join(outputDir, "%v", "index.m3u8"),
	)

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start ffmpeg: %w", err)
	}

	var totalSec float64
	pp := newProgressParser(func(current, total float64) {
		if total > 0 {
			progressFn(clamp(int(current/total*100), 0, 99))
		}
	})

	scanner := bufio.NewScanner(stderr)
	scanner.Split(splitOnCarriageReturn)
	for scanner.Scan() {
		line := scanner.Text()
		if d := parseDuration(line); d > 0 {
			totalSec = d
		}
		pp.feed(line, totalSec)
		slog.Debug("ffmpeg", "line", line)
	}

	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("ffmpeg exited: %w", err)
	}

	return &HLSResult{
		DurationSec: int(totalSec),
		HasAudio:    hasAudio,
	}, nil
}

// Thumbnail extracts a single JPEG frame from inputPath at atSec seconds
// and writes it to outputPath. If atSec exceeds the file duration the frame
// is taken at duration/2 instead.
func Thumbnail(ctx context.Context, inputPath, outputPath string, atSec int) error {
	dur := ProbeInfo(ctx, inputPath)
	ts := atSec
	if dur > 0 && ts >= dur {
		ts = dur / 2
	}
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-hide_banner", "-y",
		"-ss", strconv.Itoa(ts),
		"-i", inputPath,
		"-vframes", "1",
		"-q:v", "2",
		outputPath,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("thumbnail at %ds: %w\n%s", ts, err, string(out))
	}
	return nil
}

// ─── probe helpers ────────────────────────────────────────────────────────────

// probeFPS returns the video stream frame rate of inputPath.
func probeFPS(ctx context.Context, inputPath string) (float64, error) {
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=r_frame_rate",
		"-of", "default=nw=1:nk=1",
		inputPath)
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("ffprobe fps: %w", err)
	}
	raw := strings.TrimSpace(string(out))
	if raw == "" || raw == "0/0" {
		return 0, fmt.Errorf("empty fps from ffprobe")
	}
	parts := strings.SplitN(raw, "/", 2)
	num, _ := strconv.ParseFloat(parts[0], 64)
	if len(parts) == 1 {
		return num, nil
	}
	den, _ := strconv.ParseFloat(parts[1], 64)
	if den == 0 {
		return 0, fmt.Errorf("zero denominator in FPS %q", raw)
	}
	return num / den, nil
}

// probeHasAudio reports whether inputPath has at least one audio stream.
func probeHasAudio(ctx context.Context, inputPath string) bool {
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-select_streams", "a:0",
		"-show_entries", "stream=index",
		"-of", "csv=p=0",
		inputPath)
	out, _ := cmd.Output()
	return strings.TrimSpace(string(out)) != ""
}

// snapFPS snaps fps to the nearest common CFR target for stable keyframe alignment.
func snapFPS(fps float64) float64 {
	candidates := []float64{23.976, 24.0, 25.0, 29.97, 30.0, 50.0, 59.94, 60.0}
	best := candidates[0]
	bestDiff := math.Abs(fps - best)
	for _, c := range candidates[1:] {
		if d := math.Abs(fps - c); d < bestDiff {
			best = c
			bestDiff = d
		}
	}
	return best
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
	f, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil {
		return 0
	}
	return int(f)
}
