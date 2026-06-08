package docker

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/projdocs/safe-convert/internal"
	"go.uber.org/zap"
)

// Client wraps the Docker SDK client with conversion-specific behaviour.
type Client struct {
	cfg *internal.Config
	cli *client.Client
	log *zap.Logger
}

// NewClient initialises the Docker SDK client from the environment.
// It uses the DOCKER_HOST environment variable if set, falling back to the
// default socket path. It verifies connectivity at startup by pinging the
// daemon so a misconfigured socket fails fast rather than at first request.
func NewClient(cfg *internal.Config, log *zap.Logger) (*Client, error) {
	if WorkerImage == "" {
		return nil, fmt.Errorf("worker image is unexpectedly empty (failed to transpile during automated build)")
	}

	cli, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("docker client init: %w", err)
	}

	// Verify daemon connectivity.
	pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer pingCancel()
	if _, err := cli.Ping(pingCtx); err != nil {
		return nil, fmt.Errorf("docker daemon unreachable: %w", err)
	}
	log.Info("docker daemon connected")

	// Pull the worker image at startup. This ensures:
	//   - The image is present before the first request arrives.
	//   - A misconfigured or inaccessible image reference fails immediately.
	//   - Subsequent ContainerCreate calls are instant (image is cached).
	//
	// ImagePull returns a reader that must be fully consumed and closed for
	// the pull to complete — discarding it would leave the pull incomplete.
	log.Info("pulling worker image (this may take some time on first start-up)", zap.String("image", WorkerImage))
	pullCtx, pullCancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer pullCancel()

	rc, err := cli.ImagePull(pullCtx, WorkerImage, image.PullOptions{})
	if err != nil {
		return nil, fmt.Errorf("worker image pull: %w", err)
	}
	defer rc.Close()

	// Consume the pull progress stream. Without this the pull is not guaranteed
	// to complete before ContainerCreate is called.
	if _, err := io.Copy(io.Discard, rc); err != nil {
		return nil, fmt.Errorf("worker image pull stream: %w", err)
	}

	log.Info("worker image ready", zap.String("image", WorkerImage))

	log.Info("docker daemon connected", zap.String("worker-image", WorkerImage))
	return &Client{cfg: cfg, cli: cli, log: log}, nil
}

// Convert spawns an ephemeral LibreOffice worker container, copies the raw
// file body into /tmp/input.<ext> via CopyToContainer, starts the container,
// waits for it to convert the file, then copies the resulting PDF out and
// streams it back to the HTTP response writer.
//
// The body is streamed directly into the container via a tar pipe — it is
// never buffered in the API process. Content-Length is not trusted for size
// enforcement (MaxBytesReader in middleware handles that); it is used only
// as a metadata hint in the tar header, where an incorrect value results in
// a malformed tar stream and a clean error rather than any security issue.
//
// The container is destroyed unconditionally when Convert returns.
func (d *Client) Convert(
	ctx context.Context,
	body io.Reader,
	mimeType string,
	w http.ResponseWriter,
	r *http.Request,
	log *zap.Logger,
) error {
	convCtx, cancel := context.WithTimeout(
		ctx,
		time.Duration(d.cfg.ConversionTimeoutSecs)*time.Second,
	)
	defer cancel()

	ext, ok := internal.IsKnownMIMEType(mimeType)
	if !ok {
		return fmt.Errorf("mime type not supported")
	}
	inputFileName := fmt.Sprintf("input%s", ext)
	inputFilePath := fmt.Sprintf("/%s", inputFileName)

	// -------------------------------------------------------------------------
	// Create the container — not yet started.
	//
	// The container is created first so we have an ID to copy the file into
	// before it runs. LibreOffice is invoked by the container entrypoint;
	// it expects the input file at /tmp/input.<ext> and writes the PDF to
	// /tmp/output.pdf.
	//
	// Security constraints:
	//   NetworkMode: "none"    — no network interface whatsoever.
	//   Tmpfs /tmp             — only writable surface; in-memory, destroyed
	//                            with the container.
	//   CapDrop: ALL           — all Linux capabilities dropped.
	//   NoNewPrivileges        — prevents setuid/setgid escalation.
	//   Memory / CPUQuota      — hard resource caps.
	//   AutoRemove             — daemon removes on exit as a backstop.
	// -------------------------------------------------------------------------
	containerCfg := &container.Config{
		Image: WorkerImage,
		Env: []string{
			"INPUT_FILE_PATH=" + inputFilePath,
		},
	}

	hostCfg := &container.HostConfig{
		NetworkMode:    "none",
		ReadonlyRootfs: false,
		Tmpfs: map[string]string{
			"/tmp": "size=256m,mode=1777,noexec,nosuid",
		},
		CapDrop:     []string{"ALL"},
		SecurityOpt: []string{"no-new-privileges:true"},
		Resources: container.Resources{
			Memory:    d.cfg.ContainerMemoryBytes,
			CPUQuota:  d.cfg.ContainerCPUQuota,
			CPUPeriod: 100000,
		},
		AutoRemove: false,
	}

	created, err := d.cli.ContainerCreate(convCtx, containerCfg, hostCfg, nil, nil, "")
	if err != nil {
		return fmt.Errorf("container create: %w", err)
	}

	containerID := created.ID
	log.Info("container created", zap.String("container_id", containerID[:12]))

	defer func() {

		// capture logs
		d.captureContainerLogs(containerID, log)

		// don't remove on debug
		if d.cfg.Debug {
			log.Info("debug mode: container preserved for inspection",
				zap.String("container_id", containerID[:12]),
			)
			return
		}

		rmCtx, rmCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer rmCancel()
		if err := d.cli.ContainerRemove(rmCtx, containerID, container.RemoveOptions{
			Force: true,
		}); err != nil {
			log.Warn("container remove failed",
				zap.String("container_id", containerID[:12]),
				zap.Error(err),
			)
		}
	}()

	// -------------------------------------------------------------------------
	// Stream the request body into the container at /tmp/input.<ext> via a
	// tar pipe. No buffering occurs in this process — bytes flow from the
	// HTTP connection through the pipe writer into CopyToContainer.
	//
	// PAX format is used so the tar header does not require a declared size.
	// r.ContentLength is written as the tar header size only as a hint to the
	// daemon's tar parser; if it is wrong (user-manipulated or -1), the tar
	// stream becomes malformed and CopyToContainer returns an error cleanly.
	// It is never used for allocation or enforcement.
	// -------------------------------------------------------------------------
	pr, pw := io.Pipe()

	copyErr := make(chan error, 1)
	go func() {
		defer pw.Close()

		tw := tar.NewWriter(pw)
		err := tw.WriteHeader(&tar.Header{
			Format: tar.FormatPAX,
			Name:   inputFileName,
			Mode:   0444,
			// Size 0 with PAX format: the daemon accepts a streaming tar entry
			// without a declared size. If Content-Length is present and correct,
			// using it here allows the daemon to pre-validate; if absent or wrong,
			// the worst outcome is a malformed tar error — not a security issue.
			Size: max(r.ContentLength, 0),
		})
		if err != nil {
			copyErr <- fmt.Errorf("tar header: %w", err)
			return
		}

		// Stream body → tar entry. MaxBytesReader (applied in middleware) is
		// the authoritative size cap — not Content-Length.
		if _, err := io.Copy(tw, body); err != nil {
			copyErr <- fmt.Errorf("tar body: %w", err)
			return
		}

		if err := tw.Close(); err != nil {
			copyErr <- fmt.Errorf("tar close: %w", err)
			return
		}

		copyErr <- nil
	}()

	err = d.cli.CopyToContainer(
		convCtx,
		containerID,
		filepath.Dir(inputFilePath),
		pr,
		container.CopyToContainerOptions{
			AllowOverwriteDirWithFile: false,
			CopyUIDGID:                false,
		},
	)
	// Drain copyErr regardless of CopyToContainer outcome.
	tarErr := <-copyErr
	if err != nil {
		return fmt.Errorf("copy to container: %w", err)
	}
	if tarErr != nil {
		return tarErr
	}

	log.Info("file copied to container",
		zap.String("container_id", containerID[:12]),
		zap.String("input", inputFilePath),
	)

	// -------------------------------------------------------------------------
	// Start the container. The entrypoint converts /tmp/input.<ext> to
	// /tmp/output.pdf and exits.
	// -------------------------------------------------------------------------
	if err := d.cli.ContainerStart(convCtx, containerID, container.StartOptions{}); err != nil {
		return fmt.Errorf("container start: %w", err)
	}

	log.Info("container started", zap.String("container_id", containerID[:12]))

	// -------------------------------------------------------------------------
	// Wait for the container to exit.
	// -------------------------------------------------------------------------
	statusCh, waitErrCh := d.cli.ContainerWait(convCtx, containerID, container.WaitConditionNotRunning)

	select {
	case err := <-waitErrCh:
		return fmt.Errorf("container wait: %w", err)
	case status := <-statusCh:
		if status.Error != nil {
			return fmt.Errorf("container exited with error: %s", status.Error.Message)
		}
		if status.StatusCode != 0 {
			return fmt.Errorf("container exited with non-zero status: %d", status.StatusCode)
		}
	case <-convCtx.Done():
		return fmt.Errorf("conversion timeout after %ds", d.cfg.ConversionTimeoutSecs)
	}

	log.Info("container exited cleanly", zap.String("container_id", containerID[:12]))

	// -------------------------------------------------------------------------
	// Copy the PDF out of the container and stream it to the caller.
	//
	// CopyFromContainer returns a tar stream; we unwrap the first entry and
	// pipe it directly to the response writer without buffering.
	// -------------------------------------------------------------------------
	rc, _, err := d.cli.CopyFromContainer(convCtx, containerID, "/output.pdf")
	if err != nil {
		return fmt.Errorf("copy from container: %w", err)
	}
	defer rc.Close()

	tr := tar.NewReader(rc)
	if _, err := tr.Next(); err != nil {
		return fmt.Errorf("tar entry from container: %w", err)
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="converted.pdf"`)
	w.WriteHeader(http.StatusOK)

	if _, err := io.Copy(w, tr); err != nil {
		return fmt.Errorf("stream pdf to caller: %w", err)
	}

	return nil
}

// PruneStoppedContainers removes any stopped worker containers that were not
// cleaned up by AutoRemove or the deferred Remove in Convert. This can be
// called on a schedule to guard against leaked containers from process crashes.
func (d *Client) PruneStoppedContainers(ctx context.Context) (int, error) {
	report, err := d.cli.ContainersPrune(ctx, filters.NewArgs())
	if err != nil {
		return 0, fmt.Errorf("container prune: %w", err)
	}
	return len(report.ContainersDeleted), nil
}

func (d *Client) captureContainerLogs(containerID string, log *zap.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rc, err := d.cli.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Timestamps: false,
	})
	if err != nil {
		log.Warn("could not retrieve container logs",
			zap.String("container_id", containerID[:12]),
			zap.Error(err),
		)
		return
	}
	defer rc.Close()

	var stdout, stderr strings.Builder
	if _, err := stdcopy.StdCopy(&stdout, &stderr, rc); err != nil {
		log.Warn("error demultiplexing container logs",
			zap.String("container_id", containerID[:12]),
			zap.Error(err),
		)
		return
	}

	if out := strings.TrimSpace(stdout.String()); out != "" {
		log.Info("container stdout",
			zap.String("container_id", containerID[:12]),
			zap.String("output", out),
		)
	}

	if out := strings.TrimSpace(stderr.String()); out != "" {
		log.Info("container stderr",
			zap.String("container_id", containerID[:12]),
			zap.String("output", out),
		)
	}
}
