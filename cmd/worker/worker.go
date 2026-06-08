package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	inputDir  = "/tmp"
	outputDir = "/tmp"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "worker: %v\n", err)
		os.Exit(1)
	}
}

func run() error {

	// -------------------------------------------------------------------------
	// 1. Resolve the input file path from INPUT_FILE_PATH.
	// -------------------------------------------------------------------------
	inputPath := strings.TrimSpace(os.Getenv("INPUT_FILE_PATH"))
	outputPath := filepath.Join(outputDir, "output.pdf")

	// -------------------------------------------------------------------------
	// 2. Verify the input file exists and is readable.
	// -------------------------------------------------------------------------
	if _, err := os.Stat(inputPath); err != nil {
		return fmt.Errorf("input file not found at %s: %w", inputPath, err)
	}

	// -------------------------------------------------------------------------
	// 3. Invoke LibreOffice.
	//
	// Flags used:
	//   --headless          — no GUI; required for server-side use.
	//   --norestore         — do not attempt to restore a previous session.
	//   --nofirststartwizard — skip the first-run wizard.
	//   --nolockcheck       — do not check for a running instance lock.
	//   --convert-to pdf    — output format.
	//   --outdir            — write the PDF to /tmp.
	//
	// A per-invocation UserInstallation directory is set via
	// -env:UserInstallation so that concurrent worker containers do not
	// share LibreOffice profile state. Each container is ephemeral and
	// isolated, so this is belt-and-suspenders, but it prevents any
	// cross-contamination if the image is ever reused across requests.
	// -------------------------------------------------------------------------
	userInstall := fmt.Sprintf("file:///tmp/lo-profile-%d", time.Now().UnixNano())

	cmd := exec.Command(
		"/usr/lib/libreoffice/program/soffice",
		"--headless",
		"--norestore",
		"--nofirststartwizard",
		"--nolockcheck",
		fmt.Sprintf("-env:UserInstallation=%s", userInstall),
		"--convert-to", "pdf",
		"--outdir", outputDir,
		inputPath,
	)

	// Direct LibreOffice stdout/stderr to our stderr so the Docker daemon
	// captures it in container logs. We do not surface it to the API caller.
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("libreoffice: %w", err)
	}

	// -------------------------------------------------------------------------
	// 4. Verify the output file was produced.
	//
	// LibreOffice derives the output filename from the input stem, so
	// /tmp/input.docx → /tmp/input.pdf. We rename it to output.pdf so the
	// API always knows where to find it regardless of the input extension.
	// -------------------------------------------------------------------------
	libreofficeOutput := filepath.Join(outputDir, "input.pdf")

	if _, err := os.Stat(libreofficeOutput); err != nil {
		return fmt.Errorf("libreoffice did not produce output at %s: %w", libreofficeOutput, err)
	}

	if err := os.Rename(libreofficeOutput, outputPath); err != nil {
		return fmt.Errorf("rename output: %w", err)
	}

	fmt.Fprintf(os.Stderr, "worker: conversion complete → %s\n", outputPath)
	return nil
}
