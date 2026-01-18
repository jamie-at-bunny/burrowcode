//go:generate go run github.com/ogen-go/ogen/cmd/ogen@v1.8.1 --target oas --package oas --clean openapi.yaml

package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"ffmpeg-api/oas"

	"github.com/ghodss/yaml"
	"github.com/hibiken/asynq"
)

const TypeFFmpegCommand = "ffmpeg:command"

// WorkerCommandRequest matches the worker's expected format
type WorkerCommandRequest struct {
	InputFiles     map[string]string `json:"input_files"`
	OutputFiles    map[string]string `json:"output_files"`
	FFmpegCommand  string            `json:"ffmpeg_command,omitempty"`
	FFmpegCommands []string          `json:"ffmpeg_commands,omitempty"`
	Webhook        string            `json:"webhook,omitempty"`
	ReferenceID    string            `json:"reference_id,omitempty"`
}

// WorkerCommandResult matches the worker's result format
type WorkerCommandResult struct {
	OutputFiles             map[string]WorkerOutputFileInfo `json:"output_files"`
	FFmpegCommandRunSeconds float64                         `json:"ffmpeg_command_run_seconds"`
	TotalProcessingSeconds  float64                         `json:"total_processing_seconds"`
	CompletedAt             time.Time                       `json:"completed_at"`
}

// WorkerOutputFileInfo matches the worker's output file format
type WorkerOutputFileInfo struct {
	FileID     string  `json:"file_id"`
	SizeMBytes float64 `json:"size_mbytes"`
	FileType   string  `json:"file_type"`
	FileFormat string  `json:"file_format"`
	StorageURL string  `json:"storage_url"`
	Width      int     `json:"width,omitempty"`
	Height     int     `json:"height,omitempty"`
}

var (
	asynqClient    *asynq.Client
	asynqInspector *asynq.Inspector
	taskMaxRetry   int
	taskTimeoutMin int
	taskRetentionH int
)

// Handler implements the oas.Handler interface
type Handler struct{}

var _ oas.Handler = (*Handler)(nil)

func main() {
	redisAddr := getEnv("REDIS_ADDR", "localhost:6379")
	port := getEnv("PORT", "8080")
	taskMaxRetry = getEnvInt("TASK_MAX_RETRY", 2)
	taskTimeoutMin = getEnvInt("TASK_TIMEOUT_MINUTES", 30)
	taskRetentionH = getEnvInt("TASK_RETENTION_HOURS", 24)

	asynqClient = asynq.NewClient(asynq.RedisClientOpt{Addr: redisAddr})
	asynqInspector = asynq.NewInspector(asynq.RedisClientOpt{Addr: redisAddr})
	defer asynqClient.Close()

	handler := &Handler{}
	srv, err := oas.NewServer(handler)
	if err != nil {
		log.Fatal(err)
	}

	// Create a mux to handle OpenAPI spec separately
	mux := http.NewServeMux()
	mux.HandleFunc("/openapi.json", serveOpenAPISpec)
	mux.Handle("/", srv)

	// Wrap with CORS middleware
	corsHandler := corsMiddleware(mux)

	log.Printf("FFmpeg Command API listening on :%s", port)
	log.Printf("OpenAPI spec available at http://localhost:%s/openapi.json", port)
	log.Fatal(http.ListenAndServe(":"+port, corsHandler))
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

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-API-Key")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// HealthCheck implements the health check endpoint
func (h *Handler) HealthCheck(ctx context.Context) (*oas.HealthResponse, error) {
	return &oas.HealthResponse{Status: "ok"}, nil
}

// ListCommands returns all commands
func (h *Handler) ListCommands(ctx context.Context) (*oas.CommandListResponse, error) {
	var commands []oas.CommandStatus

	active, _ := asynqInspector.ListActiveTasks("ffmpeg", asynq.PageSize(100))
	for _, t := range active {
		commands = append(commands, taskToStatus(t, oas.CommandStatusStatusPROCESSING))
	}

	pending, _ := asynqInspector.ListPendingTasks("ffmpeg", asynq.PageSize(100))
	for _, t := range pending {
		commands = append(commands, taskToStatus(t, oas.CommandStatusStatusPENDING))
	}

	completed, _ := asynqInspector.ListCompletedTasks("ffmpeg", asynq.PageSize(100))
	for _, t := range completed {
		commands = append(commands, taskToStatus(t, oas.CommandStatusStatusSUCCESS))
	}

	archived, _ := asynqInspector.ListArchivedTasks("ffmpeg", asynq.PageSize(100))
	for _, t := range archived {
		commands = append(commands, taskToStatus(t, oas.CommandStatusStatusFAILED))
	}

	return &oas.CommandListResponse{
		Commands: commands,
		Total:    len(commands),
	}, nil
}

func taskToStatus(t *asynq.TaskInfo, status oas.CommandStatusStatus) oas.CommandStatus {
	var req WorkerCommandRequest
	json.Unmarshal(t.Payload, &req)

	cs := oas.CommandStatus{
		CommandID: t.ID,
		Status:    status,
		CreatedAt: t.NextProcessAt,
	}

	// Convert original request
	origReq := oas.CommandRequest{
		OutputFiles: oas.CommandRequestOutputFiles(req.OutputFiles),
	}
	if req.InputFiles != nil {
		origReq.InputFiles.SetTo(oas.CommandRequestInputFiles(req.InputFiles))
	}
	if req.FFmpegCommand != "" {
		origReq.FfmpegCommand.SetTo(req.FFmpegCommand)
	}
	if len(req.FFmpegCommands) > 0 {
		origReq.FfmpegCommands = req.FFmpegCommands
	}
	if req.Webhook != "" {
		if u, err := url.Parse(req.Webhook); err == nil {
			origReq.Webhook.SetTo(*u)
		}
	}
	if req.ReferenceID != "" {
		origReq.ReferenceID.SetTo(req.ReferenceID)
	}
	cs.OriginalRequest.SetTo(origReq)

	if len(t.Result) > 0 {
		var result WorkerCommandResult
		if err := json.Unmarshal(t.Result, &result); err == nil {
			outputFiles := make(oas.CommandStatusOutputFiles)
			for k, v := range result.OutputFiles {
				info := oas.OutputFileInfo{
					FileID:     v.FileID,
					SizeMbytes: v.SizeMBytes,
					FileType:   oas.OutputFileInfoFileType(v.FileType),
					FileFormat: v.FileFormat,
					StorageURL: v.StorageURL,
				}
				if v.Width > 0 {
					info.Width.SetTo(v.Width)
				}
				if v.Height > 0 {
					info.Height.SetTo(v.Height)
				}
				outputFiles[k] = info
			}
			cs.OutputFiles.SetTo(outputFiles)
			cs.FfmpegCommandRunSeconds.SetTo(result.FFmpegCommandRunSeconds)
			cs.TotalProcessingSeconds.SetTo(result.TotalProcessingSeconds)
			if !result.CompletedAt.IsZero() {
				cs.CompletedAt.SetTo(result.CompletedAt)
			}
		}
	}

	if t.LastErr != "" {
		cs.Error.SetTo(t.LastErr)
		cs.Status = oas.CommandStatusStatusFAILED
	}

	return cs
}

// CreateCommand creates a new FFmpeg command
func (h *Handler) CreateCommand(ctx context.Context, req *oas.CommandRequest) (oas.CreateCommandRes, error) {
	// Validate
	if !req.FfmpegCommand.Set && len(req.FfmpegCommands) == 0 {
		return &oas.CreateCommandBadRequest{Error: "ffmpeg_command or ffmpeg_commands required"}, nil
	}

	if len(req.OutputFiles) == 0 {
		return &oas.CreateCommandBadRequest{Error: "output_files required"}, nil
	}

	// Convert to worker format
	workerReq := WorkerCommandRequest{
		OutputFiles: map[string]string(req.OutputFiles),
	}
	if req.InputFiles.Set {
		workerReq.InputFiles = map[string]string(req.InputFiles.Value)
	}
	if req.FfmpegCommand.Set {
		workerReq.FFmpegCommand = req.FfmpegCommand.Value
	}
	if len(req.FfmpegCommands) > 0 {
		workerReq.FFmpegCommands = req.FfmpegCommands
	}
	if req.Webhook.Set {
		workerReq.Webhook = req.Webhook.Value.String()
	}
	if req.ReferenceID.Set {
		workerReq.ReferenceID = req.ReferenceID.Value
	}

	payload, _ := json.Marshal(workerReq)
	task := asynq.NewTask(TypeFFmpegCommand, payload)

	info, err := asynqClient.Enqueue(task,
		asynq.MaxRetry(taskMaxRetry),
		asynq.Timeout(time.Duration(taskTimeoutMin)*time.Minute),
		asynq.Queue("ffmpeg"),
		asynq.Retention(time.Duration(taskRetentionH)*time.Hour),
	)
	if err != nil {
		return &oas.CreateCommandInternalServerError{Error: err.Error()}, nil
	}

	resp := &oas.CommandResponse{
		CommandID: info.ID,
		Status:    oas.CommandResponseStatusPENDING,
	}
	if req.ReferenceID.Set {
		resp.ReferenceID.SetTo(req.ReferenceID.Value)
	}

	return resp, nil
}

// GetCommand returns a command by ID
func (h *Handler) GetCommand(ctx context.Context, params oas.GetCommandParams) (oas.GetCommandRes, error) {
	if params.ID == "" {
		return &oas.GetCommandBadRequest{Error: "command_id required"}, nil
	}

	info, err := asynqInspector.GetTaskInfo("ffmpeg", params.ID)
	if err != nil {
		return &oas.GetCommandNotFound{Error: "command not found"}, nil
	}

	status := stateToStatus(info.State)
	cs := taskToStatus(info, status)

	return &cs, nil
}

func stateToStatus(state asynq.TaskState) oas.CommandStatusStatus {
	switch state {
	case asynq.TaskStateActive:
		return oas.CommandStatusStatusPROCESSING
	case asynq.TaskStatePending:
		return oas.CommandStatusStatusPENDING
	case asynq.TaskStateCompleted:
		return oas.CommandStatusStatusSUCCESS
	case asynq.TaskStateArchived:
		return oas.CommandStatusStatusFAILED
	case asynq.TaskStateRetry:
		return oas.CommandStatusStatusRETRYING
	default:
		return oas.CommandStatusStatusPENDING
	}
}

// GetOpenAPI returns the OpenAPI specification
// Note: This is not used - we serve the spec via a separate handler at /openapi.json
func (h *Handler) GetOpenAPI(ctx context.Context) error {
	return nil
}

// serveOpenAPISpec serves the OpenAPI spec as JSON (converted from YAML)
func serveOpenAPISpec(w http.ResponseWriter, r *http.Request) {
	yamlData, err := os.ReadFile("openapi.yaml")
	if err != nil {
		http.Error(w, "Failed to read OpenAPI spec", http.StatusInternalServerError)
		return
	}

	jsonData, err := yaml.YAMLToJSON(yamlData)
	if err != nil {
		http.Error(w, "Failed to convert OpenAPI spec", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(jsonData)
}
