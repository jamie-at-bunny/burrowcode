package adapters

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// BunnyStorageAdapter uploads files to Bunny Edge Storage
type BunnyStorageAdapter struct {
	StorageZone     string
	StorageKey      string
	StorageEndpoint string
	PathPrefix      string
	PullZoneURL     string
}

// NewBunnyStorageAdapter creates a new Bunny Storage adapter from environment variables
func NewBunnyStorageAdapter() (*BunnyStorageAdapter, error) {
	zone := os.Getenv("BUNNY_STORAGE_ZONE")
	if zone == "" {
		return nil, fmt.Errorf("BUNNY_STORAGE_ZONE is required")
	}

	key := os.Getenv("BUNNY_STORAGE_KEY")
	if key == "" {
		return nil, fmt.Errorf("BUNNY_STORAGE_KEY is required")
	}

	return &BunnyStorageAdapter{
		StorageZone:     zone,
		StorageKey:      key,
		StorageEndpoint: getEnv("BUNNY_STORAGE_ENDPOINT", "storage.bunnycdn.com"),
		PathPrefix:      getEnv("BUNNY_STORAGE_PATH_PREFIX", ""),
		PullZoneURL:     getEnv("BUNNY_STORAGE_PULL_ZONE_URL", ""),
	}, nil
}

func (a *BunnyStorageAdapter) Name() string {
	return "bunny-storage"
}

func (a *BunnyStorageAdapter) Upload(ctx context.Context, localPath string, destPath string) (string, error) {
	// Build storage path with optional prefix
	storagePath := destPath
	if a.PathPrefix != "" {
		storagePath = strings.TrimSuffix(a.PathPrefix, "/") + "/" + destPath
	}

	url := fmt.Sprintf("https://%s/%s/%s", a.StorageEndpoint, a.StorageZone, storagePath)

	f, err := os.Open(localPath)
	if err != nil {
		return "", fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	// Get file info for content length
	stat, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("stat file: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "PUT", url, f)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("AccessKey", a.StorageKey)
	req.Header.Set("Content-Type", getContentType(localPath))
	req.ContentLength = stat.Size()

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 && resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("upload failed (status %d): %s", resp.StatusCode, string(body))
	}

	log.Printf("[bunny-storage] Uploaded %s -> %s", localPath, storagePath)

	// Return pull zone URL if configured, otherwise return storage path
	if a.PullZoneURL != "" {
		return strings.TrimSuffix(a.PullZoneURL, "/") + "/" + storagePath, nil
	}
	return storagePath, nil
}

// getContentType returns the MIME type based on file extension
func getContentType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".mp4":
		return "video/mp4"
	case ".mov":
		return "video/quicktime"
	case ".avi":
		return "video/x-msvideo"
	case ".mkv":
		return "video/x-matroska"
	case ".webm":
		return "video/webm"
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".aac":
		return "audio/aac"
	case ".flac":
		return "audio/flac"
	case ".ogg":
		return "audio/ogg"
	default:
		return "application/octet-stream"
	}
}
