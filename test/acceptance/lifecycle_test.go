// SPDX-License-Identifier: GPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package acceptance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/divbast/ci/harness"
)

// stopWithin bounds a stop: SABnzbd saves its queue and stops its threads,
// in under a second when idle, as measured.
const stopWithin = 10 * time.Second

// TestStop is a stop, as an engine or a pod's termination sends it:
// SIGTERM to PID 1, SABnzbd itself, which shuts down within stopWithin
// and exits 0.
func TestStop(t *testing.T) {
	t.Parallel()
	e, image := underTest(t)
	s, _ := serveFresh(t, e, image, nil)
	took := stop(t, e, s.C)
	harness.NewChecks(t).True("it stops within "+stopWithin.String(), took < stopWithin, took.String())
}

// TestBusyPortFails is 8080 already held in the container's network, by
// another program that answers HTTP: with the port fixed, SABnzbd stops,
// exit 2, rather than moving to another port and saving it. (A listener
// that accepts and never answers holds SABnzbd's start: it first asks the
// port whether another SABnzbd is there, and waits.)
func TestBusyPortFails(t *testing.T) {
	t.Parallel()
	e, image := underTest(t)
	holder, err := e.Start(t.Context(), t, &harness.RunSpec{
		Image: image, Network: "none", Confine: harness.Contract(tmpfsSize),
		Entrypoint: []string{python},
		Args:       []string{"-c", "import http.server, socketserver\nsocketserver.TCPServer(('0.0.0.0', 8080), http.server.BaseHTTPRequestHandler).serve_forever()"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := harness.Poll(t.Context(), 30*time.Second, 200*time.Millisecond, func(ctx context.Context) error {
		res, err := holder.Exec(ctx, nil, python, "-c", "import socket; socket.create_connection(('127.0.0.1', 8080), 1)")
		if err != nil || res.Code != 0 {
			return harness.NotYet(errors.New("8080 not held yet"))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	harness.Refuses(t, e, &harness.RunSpec{
		Image: image, Network: "container:" + holder.ID, Confine: harness.Contract(tmpfsSize),
		Mounts: []harness.Mount{configVolume(t, e)},
	}, 2, "8080 held by another program", "Unable to bind to port 8080")
}

// TestPersists is a setting changed through the API, which survives the
// container's restart: it is in /config.
func TestPersists(t *testing.T) {
	t.Parallel()
	e, image := underTest(t)
	s, _ := serveFresh(t, e, image, nil)
	key := apiKey(t, s)
	api(t, s, key, url.Values{"mode": {"set_config"}, "section": {"misc"}, "keyword": {"refresh_rate"}, "value": {"7"}}, nil)
	if err := s.Restart(t.Context()); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Config struct {
			Misc struct {
				RefreshRate json.Number `json:"refresh_rate"`
			}
		}
	}
	api(t, s, key, url.Values{"mode": {"get_config"}, "section": {"misc"}, "keyword": {"refresh_rate"}}, &got)
	harness.Equal(harness.NewChecks(t), "refresh_rate survives the restart", "7", got.Config.Misc.RefreshRate.String())
}

// restartArgv is PID 1's argv once SABnzbd has restarted itself: it keeps
// -f alone of the arguments it was given, and listens where its settings
// say, which the first start saved from -s.
var restartArgv = []string{"/app/venv/bin/python", "-OO", "/app/sabnzbd/SABnzbd.py", "-f", "/config"}

// cmdline is PID 1's argv, read in the container.
const cmdline = "import sys; sys.stdout.write('\\n'.join(open('/proc/1/cmdline').read().rstrip('\\0').split('\\0')))"

// TestRestartKeepsPID1 is the web UI's Restart: SABnzbd execs itself again,
// in place, so PID 1 stays SABnzbd, and the container comes back healthy,
// served from outside on 8080 as before, without the engine restarting it.
func TestRestartKeepsPID1(t *testing.T) {
	t.Parallel()
	e, image := underTest(t)
	s, _ := serveFresh(t, e, image, nil)
	checks := harness.NewChecks(t)
	before, err := s.C.Inspect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	api(t, s, apiKey(t, s), url.Values{"mode": {"restart"}}, nil)
	// It answers once it has started again: wait past the old process
	if err = harness.Pause(t.Context(), 2*time.Second); err != nil {
		t.Fatal(err)
	}
	if err = s.Wait(t.Context(), versionPath); err != nil {
		t.Fatalf("SABnzbd never answered again after the restart: %v", err)
	}
	res, err := s.C.Exec(t.Context(), nil, python, "-c", cmdline)
	if err != nil {
		t.Fatal(err)
	}
	harness.Equal(checks, "PID 1 is SABnzbd, restarted", strings.Join(restartArgv, "\n"), string(res.Stdout))
	after, err := s.C.Inspect(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	harness.Equal(checks, "the engine never restarted the container", before.State.StartedAt, after.State.StartedAt)
	checks.True("the container kept running", after.State.Running, after.State.Status)
	healthy(t, s, "the shipped probe passes after the restart")
}

// nzbOf writes an NZB of about size bytes, in t's temporary directory:
// well-formed, of segments no server will ever be asked for.
func nzbOf(t *testing.T, size int) string {
	t.Helper()
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?>` + "\n" +
		`<nzb xmlns="http://www.newzbin.com/DTD/2003/nzb">` + "\n")
	for f, n := 0, 0; b.Len() < size; f++ {
		fmt.Fprintf(&b, `<file poster="t@example.invalid" date="1700000000" subject="&quot;f%d.bin&quot; yEnc (1/400)"><groups><group>alt.binaries.test</group></groups><segments>`+"\n", f)
		for k := 1; k <= 400; k++ {
			n++
			fmt.Fprintf(&b, `<segment bytes="768000" number="%d">p%010dx%040d@example.invalid</segment>`+"\n", k, n, n)
		}
		b.WriteString("</segments></file>\n")
	}
	b.WriteString("</nzb>\n")
	path := filepath.Join(t.TempDir(), fmt.Sprintf("%d.nzb", size))
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// upload posts the NZB at path to SABnzbd's API, mode=addfile, as a
// browser or an indexer client sends one, and returns the answer.
func upload(t *testing.T, s *harness.Serving, key, path string) (harness.Response, error) {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for k, v := range map[string]string{"mode": "addfile", "apikey": key, "output": "json"} {
		if err := w.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
	part, err := w.CreateFormFile("name", filepath.Base(path))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path) //nolint:gosec // G304: the test's own temporary file
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return s.Do(t.Context(), nil, http.MethodPost, "/api", http.Header{"Content-Type": {w.FormDataContentType()}}, body.Bytes())
}

// The NZB uploads the README promises, and one past what /tmp takes.
const (
	promisedUpload = 50 << 20
	tooLargeUpload = 100 << 20
)

// TestUploadSizes is an NZB upload near /tmp's size: SABnzbd stages it
// there twice. The largest the README promises is accepted; one twice
// that is refused with SABnzbd's own error, and the container stays
// healthy.
func TestUploadSizes(t *testing.T) {
	t.Parallel()
	e, image := underTest(t)
	s, _ := serveFresh(t, e, image, nil)
	key := apiKey(t, s)
	checks := harness.NewChecks(t)

	resp, err := upload(t, s, key, nzbOf(t, promisedUpload))
	harness.Answers(t, "a 50 MB NZB upload answers 200", http.StatusOK, resp, err)
	var added struct {
		Status bool
		NzoIDs []string `json:"nzo_ids"`
	}
	if err == nil {
		checks.True("SABnzbd takes the 50 MB NZB", json.Unmarshal(resp.Body, &added) == nil && added.Status && len(added.NzoIDs) == 1, harness.Clip(string(resp.Body)))
	}

	resp, err = upload(t, s, key, nzbOf(t, tooLargeUpload))
	if err != nil {
		checks.True("a 100 MB NZB upload gets an answer", false, err.Error())
	} else {
		// SABnzbd's own refusal: its JSON, status false, and its warning
		// naming the file, which the web UI shows
		var refused struct{ Status *bool }
		checks.True("SABnzbd answers the 100 MB NZB with status false",
			json.Unmarshal(resp.Body, &refused) == nil && refused.Status != nil && !*refused.Status,
			fmt.Sprintf("%d %s", resp.Status, harness.Clip(string(resp.Body))))
		var got struct {
			Warnings []struct{ Text string } `json:"warnings"`
		}
		api(t, s, key, url.Values{"mode": {"warnings"}}, &got)
		name := fmt.Sprintf("Invalid NZB file %d.nzb", tooLargeUpload)
		checks.True("SABnzbd warns it skipped the 100 MB NZB",
			slices.ContainsFunc(got.Warnings, func(w struct{ Text string }) bool { return strings.Contains(w.Text, name) }),
			harness.Clip(fmt.Sprint(got.Warnings)))
	}
	healthy(t, s, "the shipped probe passes after a refused upload")
}
