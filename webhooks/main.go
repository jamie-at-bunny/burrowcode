package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/hibiken/asynq"
)

const TypeWebhookDeliver = "webhook:deliver"

type WebhookPayload struct {
	URL       string         `json:"url"`
	CommandID string         `json:"command_id"`
	Status    string         `json:"status"`
	Body      map[string]any `json:"body"`
}

var httpClient *http.Client

func main() {
	redisAddr := getEnv("REDIS_ADDR", "localhost:6379")
	concurrency := getEnvInt("CONCURRENCY", 10)
	httpTimeout := getEnvInt("HTTP_TIMEOUT", 10)
	healthPort := getEnv("HEALTH_PORT", "8081")

	httpClient = &http.Client{
		Timeout: time.Duration(httpTimeout) * time.Second,
	}

	// Start health check server in background
	go startHealthServer(healthPort)

	srv := asynq.NewServer(
		asynq.RedisClientOpt{Addr: redisAddr},
		asynq.Config{
			Concurrency: concurrency,
			Queues:      map[string]int{"webhooks": 1},
		},
	)

	mux := asynq.NewServeMux()
	mux.HandleFunc(TypeWebhookDeliver, handleWebhookDeliver)

	log.Printf("Webhook Service started (concurrency=%d, http_timeout=%ds, health_port=%s)", concurrency, httpTimeout, healthPort)
	if err := srv.Run(mux); err != nil {
		log.Fatal(err)
	}
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if intVal, err := strconv.Atoi(val); err == nil {
			return intVal
		}
	}
	return defaultVal
}

func startHealthServer(port string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	log.Printf("Health endpoint listening on :%s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Printf("Health server error: %v", err)
	}
}

func handleWebhookDeliver(ctx context.Context, t *asynq.Task) error {
	var payload WebhookPayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		return fmt.Errorf("unmarshal payload: %w", err)
	}

	start := time.Now()

	body, err := json.Marshal(payload.Body)
	if err != nil {
		return fmt.Errorf("marshal body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", payload.URL, strings.NewReader(string(body)))
	if err != nil {
		log.Printf("[%s] error=%q", payload.CommandID, err.Error())
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	duration := time.Since(start)

	if err != nil {
		log.Printf("[%s] duration=%s error=%q", payload.CommandID, duration, err.Error())
		return fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	// Drain and discard response body to allow connection reuse
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("[%s] duration=%s status=%d", payload.CommandID, duration, resp.StatusCode)
		return fmt.Errorf("webhook returned non-2xx status: %d", resp.StatusCode)
	}

	log.Printf("[%s] duration=%s status=%d success=true", payload.CommandID, duration, resp.StatusCode)
	return nil
}
