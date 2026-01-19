package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"ffmpeg-worker/adapters"

	"github.com/hibiken/asynq"
)

const (
	TypeFFmpegCommand  = "ffmpeg:command"
	TypeWebhookDeliver = "webhook:deliver"
)

type CommandRequest struct {
	InputFiles     map[string]string `json:"input_files"`
	OutputFiles    map[string]string `json:"output_files"`
	FFmpegCommand  string            `json:"ffmpeg_command,omitempty"`
	FFmpegCommands []string          `json:"ffmpeg_commands,omitempty"`
	Webhook        string            `json:"webhook,omitempty"`
	ReferenceID    string            `json:"reference_id,omitempty"`
}

type OutputFileInfo struct {
	FileID     string  `json:"file_id"`
	SizeMBytes float64 `json:"size_mbytes"`
	FileType   string  `json:"file_type"`
	FileFormat string  `json:"file_format"`
	StorageURL string  `json:"storage_url"`
	Width      int     `json:"width,omitempty"`
	Height     int     `json:"height,omitempty"`
}

type CommandResult struct {
	OutputFiles             map[string]OutputFileInfo `json:"output_files"`
	FFmpegCommandRunSeconds float64                   `json:"ffmpeg_command_run_seconds"`
	TotalProcessingSeconds  float64                   `json:"total_processing_seconds"`
	CompletedAt             time.Time                 `json:"completed_at"`
}

type WebhookPayload struct {
	URL       string         `json:"url"`
	CommandID string         `json:"command_id"`
	Status    string         `json:"status"`
	Body      map[string]any `json:"body"`
}

var (
	workDir             string
	storageAdapter      adapters.OutputAdapter
	webhookClient       *asynq.Client
	webhookMaxRetry     int
	webhookRetentionHrs int
)

func main() {
	workDir = getEnv("WORK_DIR", "/tmp/ffmpeg-jobs")
	webhookMaxRetry = getEnvInt("WEBHOOK_MAX_RETRY", 5)
	webhookRetentionHrs = getEnvInt("WEBHOOK_RETENTION_HOURS", 72)
	os.MkdirAll(workDir, 0755)

	// Initialize storage adapter
	var err error
	storageAdapter, err = adapters.NewAdapter()
	if err != nil {
		log.Fatalf("Failed to initialize storage adapter: %v", err)
	}
	log.Printf("Storage adapter: %s", storageAdapter.Name())

	redisAddr := getEnv("REDIS_ADDR", "localhost:6379")
	concurrency := getEnvInt("CONCURRENCY", 2)

	// Create asynq client for enqueueing webhook tasks
	webhookClient = asynq.NewClient(asynq.RedisClientOpt{Addr: redisAddr})
	defer webhookClient.Close()

	srv := asynq.NewServer(
		asynq.RedisClientOpt{Addr: redisAddr},
		asynq.Config{
			Concurrency: concurrency,
			Queues:      map[string]int{"ffmpeg": 1},
		},
	)

	mux := asynq.NewServeMux()
	mux.HandleFunc(TypeFFmpegCommand, handleFFmpegCommand)

	log.Printf("FFmpeg Command Worker started (concurrency=%d)", concurrency)
	if err := srv.Run(mux); err != nil {
		log.Fatal(err)
	}
}

func getEnv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func getEnvInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if intVal, err := strconv.Atoi(val); err == nil {
			return intVal
		}
	}
	return defaultVal
}

func handleFFmpegCommand(ctx context.Context, t *asynq.Task) error {
	var req CommandRequest
	if err := json.Unmarshal(t.Payload(), &req); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}

	commandID := t.ResultWriter().TaskID()
	jobDir := filepath.Join(workDir, commandID)
	os.MkdirAll(jobDir, 0755)
	defer os.RemoveAll(jobDir)

	log.Printf("[%s] Starting command with %d inputs, %d outputs", commandID, len(req.InputFiles), len(req.OutputFiles))
	startTime := time.Now()

	// Download input files
	inputPaths := make(map[string]string)
	for key, url := range req.InputFiles {
		ext := filepath.Ext(url)
		if ext == "" || len(ext) > 5 {
			ext = ".mp4"
		}
		localPath := filepath.Join(jobDir, key+ext)

		if err := downloadFile(ctx, url, localPath); err != nil {
			return fmt.Errorf("download %s: %w", key, err)
		}
		inputPaths[key] = localPath
		log.Printf("[%s] Downloaded %s: %s", commandID, key, url)
	}

	if ctx.Err() != nil {
		return fmt.Errorf("command cancelled")
	}

	// Prepare output paths
	outputPaths := make(map[string]string)
	for key, filename := range req.OutputFiles {
		outputPaths[key] = filepath.Join(jobDir, filename)
	}

	// Get commands to run
	var commands []string
	if len(req.FFmpegCommands) > 0 {
		commands = req.FFmpegCommands
	} else if req.FFmpegCommand != "" {
		commands = []string{req.FFmpegCommand}
	}

	// Execute each command
	ffmpegStart := time.Now()
	for i, cmd := range commands {
		// Replace placeholders with actual paths
		expandedCmd := expandPlaceholders(cmd, inputPaths, outputPaths)

		log.Printf("[%s] Running command %d/%d: ffmpeg %s", commandID, i+1, len(commands), expandedCmd)

		// Parse command into args (respecting quotes)
		args := parseCommandArgs(expandedCmd)
		args = append([]string{"-y"}, args...) // Always overwrite

		execCmd := exec.CommandContext(ctx, "ffmpeg", args...)
		output, err := execCmd.CombinedOutput()
		if err != nil {
			if ctx.Err() != nil {
				return fmt.Errorf("command cancelled during encoding")
			}
			return fmt.Errorf("ffmpeg failed (command %d): %w\n%s", i+1, err, string(output))
		}
	}
	ffmpegDuration := time.Since(ffmpegStart).Seconds()

	if ctx.Err() != nil {
		return fmt.Errorf("command cancelled")
	}

	// Process and upload outputs
	outputFiles := make(map[string]OutputFileInfo)
	for key, localPath := range outputPaths {
		stat, err := os.Stat(localPath)
		if err != nil {
			return fmt.Errorf("output %s not created: %w", key, err)
		}

		filename := req.OutputFiles[key]
		destPath := commandID + "_" + filename

		// Upload to storage adapter
		storageURL, err := storageAdapter.Upload(ctx, localPath, destPath)
		if err != nil {
			return fmt.Errorf("upload output %s: %w", key, err)
		}

		ext := strings.TrimPrefix(filepath.Ext(filename), ".")
		fileType := getFileType(ext)

		info := OutputFileInfo{
			FileID:     fmt.Sprintf("%s_%s", commandID, key),
			SizeMBytes: float64(stat.Size()) / (1024 * 1024),
			FileType:   fileType,
			FileFormat: ext,
			StorageURL: storageURL,
		}

		// Get dimensions for video/image (from local file before cleanup)
		if fileType == "video" || fileType == "image" {
			w, h := getMediaDimensions(localPath)
			info.Width = w
			info.Height = h
		}

		outputFiles[key] = info
		log.Printf("[%s] Output %s: %s (%.2f MB)", commandID, key, storageURL, info.SizeMBytes)
	}

	totalDuration := time.Since(startTime).Seconds()

	result := CommandResult{
		OutputFiles:             outputFiles,
		FFmpegCommandRunSeconds: ffmpegDuration,
		TotalProcessingSeconds:  totalDuration,
		CompletedAt:             time.Now(),
	}

	if req.Webhook != "" {
		enqueueWebhook(req.Webhook, commandID, &result, &req)
	}

	resultBytes, _ := json.Marshal(result)
	t.ResultWriter().Write(resultBytes)

	log.Printf("[%s] Completed in %.2fs (ffmpeg: %.2fs)", commandID, totalDuration, ffmpegDuration)
	return nil
}

func enqueueWebhook(url, commandID string, result *CommandResult, req *CommandRequest) {
	webhookBody := map[string]any{
		"command_id":                 commandID,
		"status":                     "SUCCESS",
		"output_files":               result.OutputFiles,
		"original_request":           req,
		"ffmpeg_command_run_seconds": result.FFmpegCommandRunSeconds,
		"total_processing_seconds":   result.TotalProcessingSeconds,
	}

	payload := WebhookPayload{
		URL:       url,
		CommandID: commandID,
		Status:    "SUCCESS",
		Body:      webhookBody,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[%s] Failed to marshal webhook payload: %v", commandID, err)
		return
	}

	task := asynq.NewTask(TypeWebhookDeliver, payloadBytes)
	info, err := webhookClient.Enqueue(task,
		asynq.MaxRetry(webhookMaxRetry),
		asynq.Queue("webhooks"),
		asynq.Retention(time.Duration(webhookRetentionHrs)*time.Hour),
	)
	if err != nil {
		log.Printf("[%s] Failed to enqueue webhook: %v", commandID, err)
		return
	}

	log.Printf("[%s] Webhook enqueued: %s", commandID, info.ID)
}

func expandPlaceholders(cmd string, inputs, outputs map[string]string) string {
	result := cmd
	for key, path := range inputs {
		placeholder := "{{" + key + "}}"
		result = strings.ReplaceAll(result, placeholder, path)
	}
	for key, path := range outputs {
		placeholder := "{{" + key + "}}"
		result = strings.ReplaceAll(result, placeholder, path)
	}
	return result
}

func parseCommandArgs(cmd string) []string {
	// Simple parsing that respects quoted strings
	var args []string
	var current strings.Builder
	inQuote := false
	quoteChar := rune(0)

	for _, r := range cmd {
		switch {
		case (r == '"' || r == '\'') && !inQuote:
			inQuote = true
			quoteChar = r
		case r == quoteChar && inQuote:
			inQuote = false
			quoteChar = 0
		case r == ' ' && !inQuote:
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(r)
		}
	}
	if current.Len() > 0 {
		args = append(args, current.String())
	}
	return args
}

func getFileType(ext string) string {
	ext = strings.ToLower(ext)
	switch ext {
	case "jpg", "jpeg", "png", "gif", "webp", "bmp", "tiff":
		return "image"
	case "mp4", "mov", "avi", "mkv", "webm", "flv", "wmv":
		return "video"
	case "mp3", "wav", "aac", "flac", "ogg", "m4a":
		return "audio"
	case "srt", "vtt", "ass", "ssa":
		return "subtitle"
	default:
		return "file"
	}
}

func getMediaDimensions(path string) (int, int) {
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height",
		"-of", "csv=p=0:s=x",
		path,
	)
	output, err := cmd.Output()
	if err != nil {
		return 0, 0
	}

	parts := strings.Split(strings.TrimSpace(string(output)), "x")
	if len(parts) != 2 {
		return 0, 0
	}

	width, _ := strconv.Atoi(parts[0])
	height, _ := strconv.Atoi(parts[1])
	return width, height
}

func downloadFile(ctx context.Context, url, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	return err
}
