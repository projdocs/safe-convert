package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const outputDir = "/tmp"

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
	if inputPath == "" {
		return fmt.Errorf("INPUT_FILE_PATH is not set")
	}

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
	//   --headless           — no GUI; required for server-side use.
	//   --norestore          — do not attempt to restore a previous session.
	//   --nofirststartwizard — skip the first-run wizard.
	//   --nolockcheck        — do not check for a running instance lock.
	//   --convert-to pdf     — output format.
	//   --outdir             — write the PDF to /tmp.
	//
	// A per-invocation UserInstallation directory is set via
	// -env:UserInstallation so that concurrent worker containers do not
	// share LibreOffice profile state.
	// -------------------------------------------------------------------------
	userInstall := fmt.Sprintf("file:///tmp/lo-profile-%d", os.Getpid())

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

	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("libreoffice: %w", err)
	}

	// -------------------------------------------------------------------------
	// 4. Rename LibreOffice output to the well-known path /tmp/output.pdf.
	//
	// LibreOffice derives the output filename from the input stem, so
	// /input.docx → /tmp/input.pdf. We derive the stem dynamically from
	// inputPath rather than hardcoding "input" so this works regardless of
	// what filename the API chose.
	// -------------------------------------------------------------------------
	stem := strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))
	libreofficeOutput := filepath.Join(outputDir, stem+".pdf")

	if _, err := os.Stat(libreofficeOutput); err != nil {
		return fmt.Errorf("libreoffice did not produce output at %s: %w", libreofficeOutput, err)
	}

	// -------------------------------------------------------------------------
	// 5. Copy the PDF from tmpfs to the container's writable root layer.
	//
	// The tmpfs at /tmp is destroyed when the container stops. CopyFromContainer
	// runs on a stopped container, so the output must be on the writable layer
	// before we exit. We copy (not rename) so that if the copy fails the
	// original on tmpfs is still intact for debugging.
	// -------------------------------------------------------------------------
	persistedOutput := "/output.pdf"

	in, err := os.Open(libreofficeOutput)
	if err != nil {
		return fmt.Errorf("open output for copy: %w", err)
	}
	defer in.Close()

	out, err := os.Create(persistedOutput)
	if err != nil {
		return fmt.Errorf("create persisted output: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy output to writable layer: %w", err)
	}

	if err := out.Close(); err != nil {
		return fmt.Errorf("close persisted output: %w", err)
	}

	fmt.Fprintf(os.Stderr, "worker: conversion complete → %s\n", persistedOutput)
	return nil
}
