// SPDX-License-Identifier: GPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package acceptance

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/divbast/ci/contract"
	"github.com/divbast/ci/harness"
)

// What the image's contract says of every container here.
const (
	// appPort serves SABnzbd's web UI and API, and its health with them.
	appPort = 8080
	// tmpfsSize is the size of /tmp the README states. SABnzbd stages an
	// NZB upload there twice, as the request's spool and as its own
	// copy, so 128 MiB takes the README's 50 MB uploads with room left.
	// Every container gets exactly this, so no check passes on more room
	// than the contract grants.
	tmpfsSize = "128m"
)

// binary is the image's command, which the ENTRYPOINT, the HEALTHCHECK and
// the contract's Exec all run.
const binary = "/usr/local/bin/divbast-sabnzbd"

// serverArgv is PID 1's argv: SABnzbd, which the command execs once it has
// written the first settings, listening on both families.
var serverArgv = []string{
	"/app/venv/bin/python", "-OO", "/app/sabnzbd/SABnzbd.py",
	"-f", "/config", "-s", ":::8080", "-b", "0", "--disable-file-log",
}

// healthCmd is the shipped health probe, as the image's HEALTHCHECK runs
// it and a Podman user passes it with --health-cmd.
var healthCmd = []string{binary, "health"}

// versionPath is SABnzbd's version, which needs no API key: the health
// probe's and the ready probe's path.
const versionPath = "/api?mode=version"

// TestContract runs divbast's confinement contract against the image, on a
// network with no route out: no capability, no new privileges, a seccomp
// filter, a read-only root and no writable mount that can execute, uid
// 65532, SABnzbd as PID 1, no shell and no package manager, one listener,
// no egress, and the HEALTHCHECK. Health is on the app port: SABnzbd's own
// version answer, which the probe holds to the image's version.
func TestContract(t *testing.T) {
	t.Parallel()
	e, image := underTest(t)
	contract.Run(t, &contract.Config{
		Image:      image,
		AppPort:    appPort,
		HealthPort: 0,
		HealthPath: versionPath,
		TmpfsSize:  tmpfsSize,
		PID1:       serverArgv,
		Exec:       []string{binary, "version"},
		HealthCmd:  healthCmd,
		Healthcheck: contract.Healthcheck{
			Test:        slices.Concat([]string{"CMD"}, healthCmd),
			Interval:    30 * time.Second,
			Timeout:     10 * time.Second,
			StartPeriod: 60 * time.Second,
			Retries:     3,
		},
		Mounts: func(tb testing.TB) []harness.Mount {
			tb.Helper()
			return []harness.Mount{configVolume(tb, e)}
		},
		ReadyProbe: versionOK,
	})
}

// versionOK waits for SABnzbd to answer its version from inside the
// container: it listens only once it has read its settings and queue.
func versionOK(ctx context.Context, _ *harness.Container, p *harness.Probe) error {
	url := fmt.Sprintf("http://127.0.0.1:%d%s", appPort, versionPath)
	err := harness.Poll(ctx, 0, time.Second, func(ctx context.Context) error {
		resp, err := p.HTTP(ctx, http.MethodGet, url, nil, "")
		switch {
		case err != nil:
			return harness.NotYet(err)
		case resp.Status != http.StatusOK:
			return harness.NotYet(fmt.Errorf("status %d", resp.Status))
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("GET %s never answered 200: %w", url, err)
	}
	return nil
}
