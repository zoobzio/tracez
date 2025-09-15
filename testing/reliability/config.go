package reliability

import (
	"os"
	"strconv"
	"time"
)

// ReliabilityConfig holds configuration for reliability testing
type ReliabilityConfig struct {
	Level            string        // "basic" or "stress"
	Duration         time.Duration // Test duration for stress tests
	MaxGoroutines    int          // Maximum goroutines for concurrent tests
	MaxMemoryMB      int          // Memory limit for tests
	FailureThreshold float64      // Failure rate threshold (0.0-1.0)
}

// getReliabilityConfig reads configuration from environment variables
func getReliabilityConfig() ReliabilityConfig {
	config := ReliabilityConfig{
		Level:            getEnv("TRACEZ_RELIABILITY_LEVEL", ""),
		Duration:         parseDuration(getEnv("TRACEZ_RELIABILITY_DURATION", "30s")),
		MaxGoroutines:    parseInt(getEnv("TRACEZ_RELIABILITY_MAX_GOROUTINES", "100")),
		MaxMemoryMB:      parseInt(getEnv("TRACEZ_RELIABILITY_MAX_MEMORY_MB", "512")),
		FailureThreshold: parseFloat(getEnv("TRACEZ_RELIABILITY_FAILURE_THRESHOLD", "0.05")),
	}
	
	return config
}

// getEnv returns environment variable value or default
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// parseInt parses integer from string with default fallback
func parseInt(s string) int {
	if value, err := strconv.Atoi(s); err == nil {
		return value
	}
	return 0
}

// parseFloat parses float from string with default fallback
func parseFloat(s string) float64 {
	if value, err := strconv.ParseFloat(s, 64); err == nil {
		return value
	}
	return 0.0
}

// parseDuration parses duration from string with default fallback
func parseDuration(s string) time.Duration {
	if duration, err := time.ParseDuration(s); err == nil {
		return duration
	}
	return 30 * time.Second
}

// isStressTestEnabled checks if stress testing is enabled
func isStressTestEnabled() bool {
	level := os.Getenv("TRACEZ_RELIABILITY_LEVEL")
	return level == "stress"
}

// isBasicTestEnabled checks if basic reliability testing is enabled
func isBasicTestEnabled() bool {
	level := os.Getenv("TRACEZ_RELIABILITY_LEVEL")
	return level == "basic"
}

// shouldSkipReliabilityTests determines if reliability tests should be skipped
func shouldSkipReliabilityTests() bool {
	level := os.Getenv("TRACEZ_RELIABILITY_LEVEL")
	return level == ""
}