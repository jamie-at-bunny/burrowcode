package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
)

// BunnyStreamAdapter uploads videos to Bunny Stream
type BunnyStreamAdapter struct {
	LibraryID string
	APIKey    string
}

// NewBunnyStreamAdapter creates a new Bunny Stream adapter from environment variables
func NewBunnyStreamAdapter() (*BunnyStreamAdapter, error) {
	libraryID := os.Getenv("BUNNY_STREAM_LIBRARY_ID")
	if libraryID == "" {
		return nil, fmt.Errorf("BUNNY_STREAM_LIBRARY_ID is required")
	}

	apiKey := os.Getenv("BUNNY_STREAM_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("BUNNY_STREAM_API_KEY is required")
	}

	return &BunnyStreamAdapter{
		LibraryID: libraryID,
		APIKey:    apiKey,
	}, nil
}

func (a *BunnyStreamAdapter) Name() string {
	return "bunny-stream"
}

func (a *BunnyStreamAdapter) Upload(ctx context.Context, localPath string, destPath string) (string, error) {
	// Step 1: Create video entry
	createURL := fmt.Sprintf("https://video.bunnycdn.com/library/%s/videos", a.LibraryID)

	body, _ := json.Marshal(map[string]string{"title": destPath})
	req, err := http.NewRequestWithContext(ctx, "POST", createURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("AccessKey", a.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("create video: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("create video failed (status %d): %s", resp.StatusCode, string(respBody))
	}

	var createResp struct {
		GUID string `json:"guid"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&createResp); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if createResp.GUID == "" {
		return "", fmt.Errorf("no guid returned from video creation")
	}

	// Step 2: Upload video file
	uploadURL := fmt.Sprintf("https://video.bunnycdn.com/library/%s/videos/%s", a.LibraryID, createResp.GUID)

	f, err := os.Open(localPath)
	if err != nil {
		return "", fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	req, err = http.NewRequestWithContext(ctx, "PUT", uploadURL, f)
	if err != nil {
		return "", fmt.Errorf("create upload request: %w", err)
	}
	req.Header.Set("AccessKey", a.APIKey)
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("upload failed (status %d): %s", resp.StatusCode, string(respBody))
	}

	log.Printf("[bunny-stream] Uploaded %s -> %s", localPath, createResp.GUID)
	return createResp.GUID, nil
}
