package internal

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// Config holds the validated runtime configuration.
// Fields are added here only as each feature is built.
type Config struct {
	Debug bool

	// Authentication.
	AccessToken string

	// Port the HTTP server listens on.
	Port uint16

	// HTTP server timeouts.
	ReadTimeoutSecs     int
	WriteTimeoutSecs    int
	ShutdownTimeoutSecs int

	// Logging.
	LogLevel  string // debug | info | warn | error
	LogFormat string // json | text

	// File handling.
	MaxFileSizeBytes int64

	// Docker.
	ConversionTimeoutSecs int
	ContainerMemoryBytes  int64
	ContainerCPUQuota     int64 // microseconds per 100ms period (100000 = 1 CPU)
}

// LoadConfig reads exclusively from environment variables, applies defaults where
// permitted, and returns a validated Config. Returns a non-nil error if any
// value is invalid; the caller must treat this as fatal.
func LoadConfig() (*Config, error) {
	v := viper.New()

	v.SetEnvPrefix("SAFE_CONVERT")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	v.SetDefault("debug", false)
	v.SetDefault("port", 8080)
	v.SetDefault("read_timeout_secs", 60)
	v.SetDefault("write_timeout_secs", 90)
	v.SetDefault("shutdown_timeout_secs", 30)
	v.SetDefault("log_level", "info")
	v.SetDefault("log_format", "json")
	v.SetDefault("max_file_size_mb", 5)
	v.SetDefault("conversion_timeout_secs", 15)
	v.SetDefault("container_memory_mb", 512)
	v.SetDefault("container_cpu_count", 1)

	var errs []string

	// ACCESS_TOKEN is required — no default. The service must not start without
	// it. A minimum length of 32 characters is enforced; anything shorter
	// provides insufficient entropy for a bearer token.
	accessToken := strings.TrimSpace(v.GetString("access_token"))
	if accessToken == "" {
		errs = append(errs, "SAFE_CONVERT_ACCESS_TOKEN is required (generate with: openssl rand -hex 32)")
	} else if len(accessToken) < 32 {
		errs = append(errs, "SAFE_CONVERT_ACCESS_TOKEN must be at least 32 characters")
	}

	port := v.GetInt("port")
	if port < 1 || port > 65535 {
		errs = append(errs, fmt.Sprintf(
			"SAFE_CONVERT_PORT must be between 1 and 65535, got %d", port,
		))
	}

	readTimeout := v.GetInt("read_timeout_secs")
	if readTimeout < 5 || readTimeout > 300 {
		errs = append(errs, fmt.Sprintf(
			"SAFE_CONVERT_READ_TIMEOUT_SECS must be between 5 and 300, got %d", readTimeout,
		))
	}

	writeTimeout := v.GetInt("write_timeout_secs")
	if writeTimeout < 5 || writeTimeout > 600 {
		errs = append(errs, fmt.Sprintf(
			"SAFE_CONVERT_WRITE_TIMEOUT_SECS must be between 5 and 600, got %d", writeTimeout,
		))
	}

	shutdownTimeout := v.GetInt("shutdown_timeout_secs")
	if shutdownTimeout < 1 || shutdownTimeout > 120 {
		errs = append(errs, fmt.Sprintf(
			"SAFE_CONVERT_SHUTDOWN_TIMEOUT_SECS must be between 1 and 120, got %d", shutdownTimeout,
		))
	}

	logLevel := strings.ToLower(v.GetString("log_level"))
	if !isOneOf(logLevel, "debug", "info", "warn", "error") {
		errs = append(errs, fmt.Sprintf(
			"SAFE_CONVERT_LOG_LEVEL must be one of [debug, info, warn, error], got %q", logLevel,
		))
	}

	logFormat := strings.ToLower(v.GetString("log_format"))
	if !isOneOf(logFormat, "json", "text") {
		errs = append(errs, fmt.Sprintf(
			"SAFE_CONVERT_LOG_FORMAT must be one of [json, text], got %q", logFormat,
		))
	}

	// Cross-field: write timeout must exceed read timeout.
	if len(errs) == 0 && writeTimeout <= readTimeout {
		errs = append(errs, fmt.Sprintf(
			"SAFE_CONVERT_WRITE_TIMEOUT_SECS (%d) must be greater than SAFE_CONVERT_READ_TIMEOUT_SECS (%d)",
			writeTimeout, readTimeout,
		))
	}

	maxFileSizeMB := v.GetInt64("max_file_size_mb")
	if maxFileSizeMB < 1 || maxFileSizeMB > 500 {
		errs = append(errs, fmt.Sprintf(
			"SAFE_CONVERT_MAX_FILE_SIZE_MB must be between 1 and 500, got %d", maxFileSizeMB,
		))
	}

	conversionTimeout := v.GetInt("conversion_timeout_secs")
	if conversionTimeout < 5 || conversionTimeout > 300 {
		errs = append(errs, fmt.Sprintf(
			"SAFE_CONVERT_CONVERSION_TIMEOUT_SECS must be between 5 and 300, got %d", conversionTimeout,
		))
	}

	containerMemoryMB := v.GetInt64("container_memory_mb")
	if containerMemoryMB < 128 || containerMemoryMB > 4096 {
		errs = append(errs, fmt.Sprintf(
			"SAFE_CONVERT_CONTAINER_MEMORY_MB must be between 128 and 4096, got %d", containerMemoryMB,
		))
	}

	containerCPUCount := v.GetInt64("container_cpu_count")
	if containerCPUCount < 1 || containerCPUCount > 8 {
		errs = append(errs, fmt.Sprintf(
			"SAFE_CONVERT_CONTAINER_CPU_COUNT must be between 1 and 8, got %d", containerCPUCount,
		))
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("configuration errors:\n  - %s", strings.Join(errs, "\n  - "))
	}

	return &Config{
		Debug:                 v.GetBool("debug"),
		AccessToken:           accessToken,
		Port:                  uint16(port),
		ReadTimeoutSecs:       readTimeout,
		WriteTimeoutSecs:      writeTimeout,
		ShutdownTimeoutSecs:   shutdownTimeout,
		LogLevel:              logLevel,
		LogFormat:             logFormat,
		MaxFileSizeBytes:      maxFileSizeMB * 1024 * 1024,
		ConversionTimeoutSecs: conversionTimeout,
		ContainerMemoryBytes:  containerMemoryMB * 1024 * 1024,
		ContainerCPUQuota:     containerCPUCount * 100000,
	}, nil
}

// isOneOf reports whether s equals any of the candidates.
func isOneOf(s string, candidates ...string) bool {
	for _, c := range candidates {
		if s == c {
			return true
		}
	}
	return false
}
