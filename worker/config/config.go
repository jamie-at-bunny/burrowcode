package config

import (
	"os"
	"strconv"
	"time"

	"ffmpeg-worker/system"
)

// Config holds all worker configuration
type Config struct {
	// Redis configuration
	Redis RedisConfig

	// Worker configuration
	Worker WorkerConfig

	// Resource limits
	Resources ResourceConfig

	// Webhook configuration
	Webhook WebhookConfig

	// Storage configuration (handled by adapter package)
	StorageAdapter string
}

// RedisConfig holds Redis connection settings
type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

// WorkerConfig holds worker processing settings
type WorkerConfig struct {
	Concurrency        int
	WorkDir            string
	TaskMaxRetry       int
	TaskTimeoutMinutes int
	TaskRetentionHours int
}

// ResourceConfig holds resource monitoring thresholds
type ResourceConfig struct {
	Enabled          bool
	MaxMemoryPercent float64
	CheckInterval    time.Duration
}

// WebhookConfig holds webhook delivery settings
type WebhookConfig struct {
	MaxRetry       int
	RetentionHours int
	TimeoutSeconds int
}

// Load loads configuration from environment variables with sensible defaults
func Load() *Config {
	return &Config{
		Redis: RedisConfig{
			Addr:     getEnv("REDIS_ADDR", "localhost:6379"),
			Password: getEnv("REDIS_PASSWORD", ""),
			DB:       getEnvInt("REDIS_DB", 0),
		},
		Worker: WorkerConfig{
			Concurrency:        getEnvInt("CONCURRENCY", 2),
			WorkDir:            getEnv("WORK_DIR", "/tmp/ffmpeg-jobs"),
			TaskMaxRetry:       getEnvInt("TASK_MAX_RETRY", 2),
			TaskTimeoutMinutes: getEnvInt("TASK_TIMEOUT_MINUTES", 30),
			TaskRetentionHours: getEnvInt("TASK_RETENTION_HOURS", 24),
		},
		Resources: ResourceConfig{
			Enabled:          getEnvBool("RESOURCE_CHECK_ENABLED", true),
			MaxMemoryPercent: getEnvFloat("MAX_MEMORY_PERCENT", 85.0),
			CheckInterval:    time.Duration(getEnvInt("RESOURCE_CHECK_INTERVAL_SEC", 5)) * time.Second,
		},
		Webhook: WebhookConfig{
			MaxRetry:       getEnvInt("WEBHOOK_MAX_RETRY", 5),
			RetentionHours: getEnvInt("WEBHOOK_RETENTION_HOURS", 72),
			TimeoutSeconds: getEnvInt("WEBHOOK_TIMEOUT_SECONDS", 10),
		},
		StorageAdapter: getEnv("STORAGE_ADAPTER", "file"),
	}
}

// GetResourceLimits converts config to system.ResourceLimits
func (c *Config) GetResourceLimits() system.ResourceLimits {
	return system.ResourceLimits{
		MaxMemoryPercent: c.Resources.MaxMemoryPercent,
	}
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	// Add validation logic if needed
	return nil
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

func getEnvFloat(key string, defaultVal float64) float64 {
	if val := os.Getenv(key); val != "" {
		if floatVal, err := strconv.ParseFloat(val, 64); err == nil {
			return floatVal
		}
	}
	return defaultVal
}

func getEnvBool(key string, defaultVal bool) bool {
	if val := os.Getenv(key); val != "" {
		if boolVal, err := strconv.ParseBool(val); err == nil {
			return boolVal
		}
	}
	return defaultVal
}
