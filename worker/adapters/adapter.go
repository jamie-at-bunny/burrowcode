package adapters

import (
	"context"
	"fmt"
	"os"
)

// OutputAdapter defines the interface for uploading processed files to storage
type OutputAdapter interface {
	// Name returns the adapter name for logging
	Name() string
	// Upload uploads a local file to storage and returns the public URL
	Upload(ctx context.Context, localPath string, destPath string) (url string, err error)
}

// NewAdapter creates a new storage adapter based on the STORAGE_ADAPTER environment variable
func NewAdapter() (OutputAdapter, error) {
	adapterType := getEnv("STORAGE_ADAPTER", "file")

	switch adapterType {
	case "file":
		return NewFileAdapter(), nil
	case "bunny-storage":
		return NewBunnyStorageAdapter()
	case "bunny-stream":
		return NewBunnyStreamAdapter()
	case "s3":
		return NewS3Adapter()
	default:
		return nil, fmt.Errorf("unknown storage adapter: %s", adapterType)
	}
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
