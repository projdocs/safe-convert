package internal

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// Config holds the validated runtime configuration.
// Fields are added here only as each feature is built.
type Config struct {
	// Port the HTTP server listens on.
	Port uint16

	// HTTP server timeouts.
	ReadTimeoutSecs     int
	WriteTimeoutSecs    int
	ShutdownTimeoutSecs int

	// Logging.
	LogLevel  string // debug | info | warn | error
	LogFormat string // json | text
}

// LoadConfig reads exclusively from environment variables, applies defaults where
// permitted, and returns a validated Config. Returns a non-nil error if any
// value is invalid; the caller must treat this as fatal.
func LoadConfig() (*Config, error) {
	v := viper.New()

	v.SetEnvPrefix("SAFE_CONVERT")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	v.SetDefault("port", 8080)
	v.SetDefault("read_timeout_secs", 60)
	v.SetDefault("write_timeout_secs", 90)
	v.SetDefault("shutdown_timeout_secs", 30)
	v.SetDefault("log_level", "info")
	v.SetDefault("log_format", "json")

	var errs []string
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

	if len(errs) > 0 {
		return nil, fmt.Errorf("configuration errors:\n  - %s", strings.Join(errs, "\n  - "))
	}

	return &Config{
		Port:                uint16(port),
		ReadTimeoutSecs:     readTimeout,
		WriteTimeoutSecs:    writeTimeout,
		ShutdownTimeoutSecs: shutdownTimeout,
		LogLevel:            logLevel,
		LogFormat:           logFormat,
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
