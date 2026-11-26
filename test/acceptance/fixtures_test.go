// SPDX-License-Identifier: GPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package acceptance

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/divbast/ci/harness"
)

// configDir is SABnzbd's directory, and python the image's own, which the
// suite runs for what it sets up and reads inside a container.
const (
	configDir = "/config"
	python    = "/app/venv/bin/python"
)

// asRoot runs the image's Python as root, for the setup of a volume a test
// needs owned otherwise: on no network, read-only, with no new privileges.
var asRoot = harness.Confinement{User: "0:0", ReadOnly: true, NoNewPrivileges: true}

// upstreamHosts are the hosts SABnzbd reaches out to unasked: its release
// check, ten minutes after a start. Every container here resolves them to
// loopback, so no test talks to sabnzbd.org.
var upstreamHosts = []string{"sabnzbd.org", "www.sabnzbd.org", "raw.githubusercontent.com"}

// noUpstream points upstreamHosts at loopback.
func noUpstream() []string {
	extra := make([]string, 0, 2*len(upstreamHosts))
	for _, h := range upstreamHosts {
		extra = append(extra, "--add-host", h+":127.0.0.1")
	}
	return extra
}

// engineLogs keeps Podman's container logs in a file of its own, k8s-file:
// journald, Podman's default, rate-limits, and a test that reads a log
// would see lines go missing.
func engineLogs(e harness.Engine) []string {
	if e.Name() == "podman" {
		return []string{"--log-driver", "k8s-file"}
	}
	return nil
}

// configVolume is a new, empty volume at /config, which the engine gives
// the image's owner of /config, uid 65532, on its first mount.
func configVolume(tb testing.TB, e harness.Engine) harness.Mount {
	tb.Helper()
	name, err := e.Volume(tb.Context(), tb, "")
	if err != nil {
		tb.Fatal(err)
	}
	return harness.Mount{Source: name, Target: configDir, Volume: true}
}

// prepare runs code, a Python program, in the image as root, on no network,
// with vol at /config: to own it, or fill it, as a test needs. It leaves a
// file there, so the engine never re-owns the volume, which it does to an
// empty one on each mount.
func prepare(t *testing.T, e harness.Engine, image string, vol harness.Mount, code string) {
	t.Helper()
	res, err := e.Run(t.Context(), t, &harness.RunSpec{
		Image:      image,
		Network:    "none",
		Confine:    asRoot,
		Mounts:     []harness.Mount{vol},
		Entrypoint: []string{python},
		Args:       []string{"-c", "open('/config/.prepared', 'w').close()\n" + code},
	})
	if err != nil || res.Code != 0 {
		t.Fatalf("preparing %s: %v, exit %d: %s", vol.Source, err, res.Code, res.Stderr)
	}
}

// serveSAB serves SABnzbd from image under the contract, with env and
// mounts, once it answers its version.
func serveSAB(t *testing.T, e harness.Engine, image string, env map[string]string, mounts ...harness.Mount) *harness.Serving {
	t.Helper()
	s, err := e.Serve(t.Context(), t, &harness.ServeSpec{
		Run: harness.RunSpec{
			Image:     image,
			Confine:   harness.Contract(tmpfsSize),
			Env:       maps.Clone(env),
			Mounts:    mounts,
			HealthCmd: healthCmd,
			Extra:     append(noUpstream(), engineLogs(e)...),
		},
		AppPort:   appPort,
		ReadyPath: versionPath,
	})
	if err != nil {
		t.Fatalf("serving %s: %v", image, err)
	}
	return s
}

// serveFresh serves SABnzbd on a new /config volume, and returns it.
func serveFresh(t *testing.T, e harness.Engine, image string, env map[string]string) (*harness.Serving, harness.Mount) {
	t.Helper()
	vol := configVolume(t, e)
	return serveSAB(t, e, image, env, vol), vol
}

// iniPath is SABnzbd's settings file.
const iniPath = configDir + "/sabnzbd.ini"

// readINI reads SABnzbd's settings inside s's container, with the image's
// Python.
func readINI(t *testing.T, s *harness.Serving) (string, error) {
	t.Helper()
	res, err := s.C.Exec(t.Context(), nil, python, "-c", "import sys; sys.stdout.write(open(sys.argv[1]).read())", iniPath)
	if err != nil {
		return "", err
	}
	if res.Code != 0 {
		return "", fmt.Errorf("reading %s: exit %d: %s", iniPath, res.Code, res.Stderr)
	}
	return string(res.Stdout), nil
}

// apiKeyLine is SABnzbd's API key, in its settings.
var apiKeyLine = regexp.MustCompile(`(?m)^api_key = (\S+)$`)

// apiKey reads the API key SABnzbd made itself, from its settings.
func apiKey(t *testing.T, s *harness.Serving) string {
	t.Helper()
	ini, err := readINI(t, s)
	if err != nil {
		t.Fatal(err)
	}
	m := apiKeyLine.FindStringSubmatch(ini)
	if m == nil {
		t.Fatalf("sabnzbd.ini holds no api_key:\n%s", harness.Clip(ini))
	}
	return m[1]
}

// api calls SABnzbd's API with params, the API key and JSON output, and
// decodes the answer into v, which may be nil.
func api(t *testing.T, s *harness.Serving, key string, params url.Values, v any) {
	t.Helper()
	q := url.Values{"apikey": {key}, "output": {"json"}}
	maps.Copy(q, params)
	resp, err := s.Get(t.Context(), "/api?"+q.Encode(), nil)
	harness.Answers(t, "GET /api?mode="+params.Get("mode"), http.StatusOK, resp, err)
	if err == nil && v != nil {
		if err := json.Unmarshal(resp.Body, v); err != nil {
			t.Errorf("mode=%s answered no JSON: %v: %s", params.Get("mode"), err, harness.Clip(string(resp.Body)))
		}
	}
}

// imageVersion is SABnzbd's version as the Containerfile pins it: what the
// image is tagged with, and what SABnzbd must report.
func imageVersion(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("../../Containerfile")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^ARG SABNZBD_VERSION=(\S+)$`).FindSubmatch(data)
	if m == nil {
		t.Fatal("the Containerfile has no ARG SABNZBD_VERSION= line")
	}
	return string(m[1])
}

// healthy runs the shipped probe in s's container, which must print
// nothing and exit 0.
func healthy(t *testing.T, s *harness.Serving, what string) {
	t.Helper()
	checks := harness.NewChecks(t)
	res, err := s.C.Exec(t.Context(), nil, healthCmd...)
	if err != nil {
		checks.True(what, false, err.Error())
		return
	}
	out := strings.TrimSpace(string(res.Stdout) + string(res.Stderr))
	checks.True(what, res.Code == 0 && out == "", fmt.Sprintf("exit status %d, printing %s", res.Code, harness.Clip(out)))
}

// stop stops c with the engine's own stop, as a user's would, and
// returns how long it took, failing t unless it exits 0.
func stop(t *testing.T, e harness.Engine, c *harness.Container) time.Duration {
	t.Helper()
	start := time.Now()
	out, err := exec.CommandContext(t.Context(), e.Name(), "stop", "-t", "30", c.ID).CombinedOutput() //nolint:gosec // G204: the engine and the container are the harness's own
	took := time.Since(start)
	if err != nil {
		t.Fatalf("%s stop %s: %v: %s", e.Name(), c, err, out)
	}
	code, err := c.WaitExit(t.Context(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	harness.Equal(harness.NewChecks(t), "SABnzbd exits 0 when stopped", 0, code)
	return took
}
