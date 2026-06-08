package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/projdocs/safe-convert/internal"
	"github.com/projdocs/safe-convert/internal/server"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func main() {

	// minimal stderr logger for the startup phase only.
	bootstrap, _ := zap.NewProduction()
	defer bootstrap.Sync()

	// load config
	cfg, err := internal.LoadConfig()
	if err != nil {
		bootstrap.Fatal("configuration invalid", zap.Error(err))
	}

	// replace bootstrap logger with fully configured one
	log, err := buildLogger(cfg)
	if err != nil {
		bootstrap.Fatal("failed to initialise logger", zap.Error(err))
	}
	defer log.Sync()
	log.Info("safe-convert entry API starting",
		zap.Int("port", int(cfg.Port)),
		zap.String("log_level", cfg.LogLevel),
		zap.String("log_format", cfg.LogFormat),
		zap.Bool("debug", cfg.Debug),
	)

	var protocols http.Protocols
	protocols.SetHTTP1(true)
	protocols.SetHTTP2(false) // additional attack vectors & unnecessary in microservice

	httpServer := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           server.New(cfg, log),
		Protocols:         &protocols,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       time.Duration(cfg.ReadTimeoutSecs) * time.Second,
		WriteTimeout:      time.Duration(cfg.WriteTimeoutSecs) * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Start the server in a goroutine so we can block on signals below.
	serverErr := make(chan error, 1)
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	log.Info("listening", zap.String("addr", httpServer.Addr))

	// Block until we receive a termination signal or the server fails.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)

	select {
	case sig := <-quit:
		log.Info("shutdown signal received", zap.String("signal", sig.String()))
	case err := <-serverErr:
		log.Error("server error", zap.Error(err))
	}

	// Attempt graceful shutdown: stop accepting new connections and wait for
	// in-flight requests to complete within the configured window.
	shutdownCtx, cancel := context.WithTimeout(
		context.Background(),
		time.Duration(cfg.ShutdownTimeoutSecs)*time.Second,
	)
	defer cancel()

	log.Info("shutting down",
		zap.Int("timeout_secs", cfg.ShutdownTimeoutSecs),
	)

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Error("graceful shutdown failed; forcing exit", zap.Error(err))
		os.Exit(1)
	}

	log.Info("shutdown complete")
}

func buildLogger(cfg *internal.Config) (*zap.Logger, error) {
	level, err := zapcore.ParseLevel(cfg.LogLevel)
	if err != nil {
		return nil, fmt.Errorf("invalid log level %q: %w", cfg.LogLevel, err)
	}

	var zapCfg zap.Config
	if cfg.LogFormat == "json" {
		zapCfg = zap.NewProductionConfig()
		zapCfg.DisableCaller = true
		zapCfg.DisableStacktrace = true
	} else {
		zapCfg = zap.NewDevelopmentConfig()
	}

	zapCfg.Level = zap.NewAtomicLevelAt(level)
	return zapCfg.Build()
}
