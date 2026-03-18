package ingest

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
)

// Puller copies a single source file from a remote to a local directory via rclone.
type Puller struct {
	remote   string // rclone remote name, e.g. "storage"
	basePath string // base path on remote, e.g. "/incoming"
}

func NewPuller(remote, basePath string) *Puller {
	return &Puller{remote: remote, basePath: basePath}
}

// Copy copies sourcePath (as stored in incoming_media_items.source_path, e.g. "/incoming/subdir/film.mkv")
// into destDir on local disk. Returns the absolute local path of the copied file.
// rclone invocation: rclone copy {remote}:{sourcePath} {destDir} --progress --stats-one-line --stats=5s
func (p *Puller) Copy(ctx context.Context, sourcePath, destDir string) (string, error) {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", fmt.Errorf("create dest dir: %w", err)
	}
	src := fmt.Sprintf("%s:%s", p.remote, sourcePath)
	args := []string{"copy", src, destDir, "--progress", "--stats-one-line", "--stats=5s"}
	cmd := exec.CommandContext(ctx, "rclone", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	slog.Info("rclone copy starting", "remote", src, "dest", destDir)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("rclone copy %s -> %s: %w", src, destDir, err)
	}
	localPath := filepath.Join(destDir, filepath.Base(sourcePath))
	slog.Info("rclone copy complete", "local_path", localPath)
	return localPath, nil
}
