// SPDX-License-Identifier: GPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package acceptance

import (
	"archive/zip"
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/divbast/ci/harness"
)

// nativeBackup is the settings a backup from SABnzbd installed on a host
// carries: its default host, 127.0.0.1, and SSDP on.
const nativeBackup = "[misc]\nhost = 127.0.0.1\nport = 8080\nenable_broadcast = 1\n"

// csrfToken is the token SABnzbd's config pages give the browser, which
// every form they post must send back.
var csrfToken = regexp.MustCompile(`var csrfToken = '([0-9a-f]+)'`)

// restore uploads a backup holding ini as its sabnzbd.ini, as Config,
// General, Restore does, with the page's CSRF token.
func restore(t *testing.T, s *harness.Serving, ini string) (harness.Response, error) {
	t.Helper()
	page, err := s.Get(t.Context(), "/config/general", nil)
	harness.Answers(t, "GET /config/general", http.StatusOK, page, err)
	m := csrfToken.FindSubmatch(page.Body)
	if m == nil {
		t.Fatalf("/config/general holds no CSRF token: %s", harness.Clip(string(page.Body)))
	}
	var backup bytes.Buffer
	z := zip.NewWriter(&backup)
	f, err := z.Create("sabnzbd.ini")
	if err == nil {
		_, err = f.Write([]byte(ini))
	}
	if err == nil {
		err = z.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("config_backup_file", "sabnzbd_config_backup.zip")
	if err == nil {
		_, err = part.Write(backup.Bytes())
	}
	if err == nil {
		err = w.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	header := http.Header{"Content-Type": {w.FormDataContentType()}, "X-Sabnzbd-Csrf": {string(m[1])}}
	return s.Do(t.Context(), nil, http.MethodPost, "/config/general/upload_config?output=json", header, body.Bytes())
}

// TestBackupRestore is a backup from SABnzbd installed on a host, restored
// in the web UI. SABnzbd restarts itself listening where the backup says,
// 127.0.0.1 alone, which the container's port cannot reach: the shipped
// probe must fail, saying so, though SABnzbd answers inside. A restart of
// the container puts SABnzbd back on every address, as the README says,
// and SSDP stays as the backup has it, which the README says to turn off.
func TestBackupRestore(t *testing.T) {
	t.Parallel()
	e, image := underTest(t)
	s, _ := serveFresh(t, e, image, nil)
	key := apiKey(t, s)
	checks := harness.NewChecks(t)

	resp, err := restore(t, s, nativeBackup)
	harness.Answers(t, "the backup upload", http.StatusOK, resp, err)
	if err != nil || !strings.Contains(string(resp.Body), `"success":true`) {
		t.Fatalf("SABnzbd refused the backup: %s", harness.Clip(string(resp.Body)))
	}
	api(t, s, key, url.Values{"mode": {"restart"}}, nil)

	// Once SABnzbd has started again, inside, the probe says where it listens
	var probe harness.Result
	for deadline := time.Now().Add(time.Minute); time.Now().Before(deadline); {
		if err = harness.Pause(t.Context(), 2*time.Second); err != nil {
			t.Fatal(err)
		}
		if probe, err = s.C.Exec(t.Context(), nil, healthCmd...); err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(string(probe.Stderr), "listen: ") {
			break
		}
	}
	said := strings.TrimSpace(string(probe.Stdout) + string(probe.Stderr))
	checks.True("the probe fails, naming 127.0.0.1, once SABnzbd listens there alone",
		probe.Code == 1 && strings.HasPrefix(said, "listen: ") && strings.Contains(said, "127.0.0.1") && strings.Count(said, "\n") == 0,
		fmt.Sprintf("exit status %d, printing %s", probe.Code, harness.Clip(said)))
	if resp, err = s.Get(t.Context(), versionPath, nil); err == nil && resp.Status == http.StatusOK {
		checks.True("nothing outside the container reaches SABnzbd on 127.0.0.1 alone", false, harness.Clip(string(resp.Body)))
	}
	ini, err := readINI(t, s)
	if err != nil {
		t.Fatal(err)
	}
	harness.Equal(checks, "the backup's host is restored", "127.0.0.1", iniValue(ini, "host"))
	harness.Equal(checks, "the backup's SSDP is restored", "1", iniValue(ini, "enable_broadcast"))

	// A restart of the container: SABnzbd on every address again, from -s
	if err = s.Restart(t.Context()); err != nil {
		t.Fatal(err)
	}
	healthy(t, s, "the shipped probe passes after the container restarts")
	if ini, err = readINI(t, s); err != nil {
		t.Fatal(err)
	}
	harness.Equal(checks, "the restart saves host :: again", "::", iniValue(ini, "host"))
	harness.Equal(checks, "SSDP stays as the backup had it", "1", iniValue(ini, "enable_broadcast"))
}
