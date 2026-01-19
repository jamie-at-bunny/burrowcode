package adapters

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
)

// FileAdapter saves output files to the local filesystem
type FileAdapter struct {
	OutputDir      string
	StorageBaseURL string
}

// NewFileAdapter creates a new file adapter from environment variables
func NewFileAdapter() *FileAdapter {
	outputDir := getEnv("OUTPUT_DIR", "/tmp/ffmpeg-output")
	os.MkdirAll(outputDir, 0755)

	return &FileAdapter{
		OutputDir:      outputDir,
		StorageBaseURL: getEnv("STORAGE_BASE_URL", ""),
	}
}

func (a *FileAdapter) Name() string {
	return "file"
}

func (a *FileAdapter) Upload(ctx context.Context, localPath string, destPath string) (string, error) {
	finalPath := filepath.Join(a.OutputDir, destPath)

	// Ensure destination directory exists
	dir := filepath.Dir(finalPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create directory: %w", err)
	}

	src, err := os.Open(localPath)
	if err != nil {
		return "", fmt.Errorf("open source: %w", err)
	}
	defer src.Close()

	dst, err := os.Create(finalPath)
	if err != nil {
		return "", fmt.Errorf("create dest: %w", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return "", fmt.Errorf("copy: %w", err)
	}

	log.Printf("[file] Saved %s -> %s", localPath, finalPath)

	// Return URL if base URL is configured, otherwise return local path
	if a.StorageBaseURL != "" {
		return a.StorageBaseURL + "/" + destPath, nil
	}
	return finalPath, nil
}
