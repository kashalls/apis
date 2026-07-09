package trmnl

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// ImageTTL is how long an uploaded image is kept on disk before being swept.
// It can't be deleted right after the first serve: TRMNL fetches the pushed
// image_url asynchronously to render the plugin, and may re-fetch it more
// than once (device refresh cycles, retries, multiple devices on one
// webhook), so the file needs to survive long enough for those fetches.
const ImageTTL = time.Hour

// StartImageSweeper periodically deletes files under imagesDir older than
// ImageTTL. It runs until ctx is canceled.
func StartImageSweeper(ctx context.Context, imagesDir string, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				sweepImages(imagesDir)
			}
		}
	}()
}

func sweepImages(imagesDir string) {
	entries, err := os.ReadDir(imagesDir)
	if err != nil {
		slog.Warn("sweep images: read dir", "err", err)
		return
	}

	cutoff := time.Now().Add(-ImageTTL)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			slog.Warn("sweep images: stat entry", "name", entry.Name(), "err", err)
			continue
		}
		if info.ModTime().After(cutoff) {
			continue
		}

		path := filepath.Join(imagesDir, entry.Name())
		if err := os.Remove(path); err != nil {
			slog.Warn("sweep images: remove", "path", path, "err", err)
			continue
		}
		slog.Debug("swept expired image", "path", path)
	}
}
