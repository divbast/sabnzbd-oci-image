// SPDX-License-Identifier: GPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/divbast/sabnzbd-oci-image/internal/pins"
)

const commit = "ffc619947b5ba4e6520e3a0bc70680024e184b2f"

// containerfile is the start of the image's Containerfile, as the tool
// reads it.
const containerfile = `ARG SABNZBD_VERSION=5.2.0RC2
ARG SABNZBD_COMMIT=` + commit + `
ARG RUNTIME_IMAGE=dhi.io/python:3.15.2-debian13@sha256:c6c9414d78bd285be1a4dacfb94ae61732c77019605b1678792823e319b720f6
FROM dhi.io/python:3.15.2-debian13-dev@sha256:c73e2cb9e8d56cfab7f01b99f67b2c830e84e7397dfe666d8bfaf5a69ad0ce85 AS deps
FROM ${RUNTIME_IMAGE}
`

const pinsTxt = `# upstream leaves it unpinned: cryptography>=3.0
cryptography==50.0.2

# builds cryptography and ujson from source
setuptools==82.0.1
`

// upstream is a requirements.txt of the kinds SABnzbd's holds.
const upstream = `six==1.17.0
ujson==6.0.0 # a C extension
cryptography>=3.0
pywin32==312; sys_platform == 'win32'
`

// servers stands in for GitHub and PyPI, both on one server, with each
// release in yanked yanked on PyPI, for broken metadata.
func servers(t *testing.T, yanked ...string) pins.Sources {
	t.Helper()
	release := func(files ...string) []map[string]any {
		out := make([]map[string]any, 0, len(files))
		for _, f := range files {
			kind := "bdist_wheel"
			if strings.HasSuffix(f, ".tar.gz") {
				kind = "sdist"
			}
			h := sha256.Sum256([]byte(f))
			pulled := slices.ContainsFunc(yanked, func(r string) bool { return strings.HasPrefix(f, r+"-") })
			out = append(out, map[string]any{
				"filename": f, "packagetype": kind, "url": "https://files.example/" + f,
				"digests": map[string]string{"sha256": hex.EncodeToString(h[:])},
				"yanked":  pulled, "yanked_reason": map[bool]any{true: "broken metadata", false: nil}[pulled],
			})
		}
		return out
	}
	// What a release requires, as PyPI's info.requires_dist gives it: six
	// bounds pins.txt's cryptography from above, on the image's Python,
	// and asks for more only of an extra
	requires := map[string][]string{
		"/pypi/six/1.17.0/json": {`cryptography<51,>=40; python_version >= "3.10"`, `cryptography<50; extra == "old"`},
	}
	pypi := map[string][]map[string]any{
		"/pypi/six/1.17.0/json":          release("six-1.17.0-py2.py3-none-any.whl", "six-1.17.0.tar.gz"),
		"/pypi/ujson/6.0.0/json":         release("ujson-6.0.0-cp315-cp315-manylinux_2_17_x86_64.manylinux2014_x86_64.whl", "ujson-6.0.0.tar.gz"),
		"/pypi/cryptography/50.0.2/json": release("cryptography-50.0.2-cp311-abi3-manylinux_2_34_x86_64.whl", "cryptography-50.0.2.tar.gz"),
		"/pypi/setuptools/82.0.1/json":   release("setuptools-82.0.1-py3-none-any.whl", "setuptools-82.0.1.tar.gz"),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/sabnzbd/sabnzbd/"+commit+"/requirements.txt":
			_, _ = w.Write([]byte(upstream))
		case pypi[r.URL.Path] != nil:
			_ = json.NewEncoder(w).Encode(map[string]any{"urls": pypi[r.URL.Path], "info": map[string]any{"requires_dist": requires[r.URL.Path]}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return pins.Sources{PyPI: &pins.PyPI{Base: srv.URL, Client: srv.Client()}, GitHub: srv.URL, Client: srv.Client()}
}

// repo writes a repository holding the Containerfile and pins.txt.
func repo(t *testing.T) string {
	t.Helper()
	return repoWith(t, containerfile)
}

// repoWith writes a repository holding cf as its Containerfile, and pins.txt.
func repoWith(t *testing.T, cf string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "python"), 0o750); err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{"Containerfile": cf, "python/pins.txt": pinsTxt} {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(name)), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func read(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name))) //nolint:gosec // G304: the test's own temporary file
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestCheckMode(t *testing.T) {
	t.Parallel()
	src, dir := servers(t), repo(t)
	runIn := func(args ...string) (int, string, string) {
		var stdout, stderr bytes.Buffer
		code := run(t.Context(), append([]string{"-C", dir}, args...), src, &stdout, &stderr)
		return code, stdout.String(), stderr.String()
	}

	// Nothing generated yet: stale
	if code, out, _ := runIn("-check"); code != 1 || !strings.Contains(out, "+++ python/requirements.lock") {
		t.Errorf("-check before any lock = %d, %q; want 1 and a diff creating the lock", code, out)
	}
	if code, _, errs := runIn(); code != 0 {
		t.Fatalf("generate = %d; stderr: %s", code, errs)
	}
	lock := read(t, dir, "python/requirements.lock")
	for _, want := range []string{"--no-binary cryptography,ujson\n", "six==1.17.0 \\\n", "cryptography==50.0.2 \\\n", "ujson==6.0.0 \\\n"} {
		if !strings.Contains(lock, want) {
			t.Errorf("requirements.lock =\n%s\nwant it to hold %q", lock, want)
		}
	}
	if strings.Contains(lock, "pywin32") || strings.Contains(lock, "setuptools") {
		t.Errorf("requirements.lock =\n%s\nwant no Windows package and no build backend", lock)
	}
	if b := read(t, dir, "python/build.lock"); !strings.Contains(b, "setuptools==82.0.1 \\\n") {
		t.Errorf("build.lock =\n%s\nwant setuptools 82.0.1", b)
	}
	// Every file either lock lets pip take, by its sha256 and URL, and no other
	files := read(t, dir, "python/files.txt")
	for _, f := range []string{"six-1.17.0-py2.py3-none-any.whl", "ujson-6.0.0.tar.gz", "cryptography-50.0.2.tar.gz", "setuptools-82.0.1-py3-none-any.whl"} {
		h := sha256.Sum256([]byte(f))
		if want := hex.EncodeToString(h[:]) + " https://files.example/" + f + "\n"; !strings.Contains(files, want) {
			t.Errorf("files.txt =\n%s\nwant it to hold %q", files, want)
		}
	}
	if n := strings.Count(files, " https://"); n != 4 {
		t.Errorf("files.txt names %d files, want the 4 the locks take", n)
	}
	bounds := read(t, dir, "python/renovate-bounds.json")
	for _, want := range []string{
		`"description": "cryptography: upstream asks for >=3.0; six 1.17.0 requires <51,>=40."`,
		`"matchPackageNames": [` + "\n" + `        "cryptography"`,
		`"allowedVersions": "<51,>=3.0,>=40"`,
	} {
		if !strings.Contains(bounds, want) {
			t.Errorf("renovate-bounds.json =\n%s\nwant it to hold %s", bounds, want)
		}
	}
	if n := strings.Count(bounds, `"allowedVersions"`); n != 1 {
		t.Errorf("renovate-bounds.json holds %d rules; want cryptography's alone", n)
	}
	if cf := read(t, dir, "Containerfile"); cf != containerfile {
		t.Errorf("Containerfile =\n%s\nwant it left as it was", cf)
	}

	// Current: nothing to say
	if code, out, errs := runIn("-check"); code != 0 || out != "" {
		t.Errorf("-check when current = %d, %q (stderr %q); want 0 and nothing", code, out, errs)
	}

	// A lock edited by hand is stale, and -check writes nothing
	stale := strings.Replace(lock, "six==1.17.0", "six==1.16.0", 1)
	if err := os.WriteFile(filepath.Join(dir, "python", "requirements.lock"), []byte(stale), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, _ := runIn("-check")
	if code != 1 || !strings.Contains(out, "--- python/requirements.lock") || !strings.Contains(out, "-six==1.16.0 \\") || !strings.Contains(out, "+six==1.17.0 \\") {
		t.Errorf("-check of a stale lock = %d,\n%s\nwant 1 and a unified diff of the change", code, out)
	}
	if got := read(t, dir, "python/requirements.lock"); got != stale {
		t.Error("-check rewrote the lock")
	}

	// So are bounds edited by hand
	if err := os.WriteFile(filepath.Join(dir, "python", "requirements.lock"), []byte(lock), 0o600); err != nil {
		t.Fatal(err)
	}
	loosened := strings.Replace(bounds, `"<51,>=3.0,>=40"`, `">=3.0"`, 1)
	if err := os.WriteFile(filepath.Join(dir, "python", "renovate-bounds.json"), []byte(loosened), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, _ = runIn("-check")
	if code != 1 || !strings.Contains(out, "--- python/renovate-bounds.json") || !strings.Contains(out, `+      "allowedVersions": "<51,>=3.0,>=40"`) {
		t.Errorf("-check of loosened bounds = %d,\n%s\nwant 1 and a diff restoring them", code, out)
	}
}

// TestYankedUpstreamPin is a release SABnzbd pins with == that PyPI has
// since yanked: pip still installs it, so the tool keeps it, says so in
// the lock and in a note, and exits 0. One pins.txt names is refused.
func TestYankedUpstreamPin(t *testing.T) {
	t.Parallel()
	src, dir := servers(t, "six"), repo(t)
	var stdout, stderr bytes.Buffer
	if code := run(t.Context(), []string{"-C", dir}, src, &stdout, &stderr); code != 0 {
		t.Fatalf("run() = %d, stderr %q; want 0", code, stderr.String())
	}
	if msg := stderr.String(); !strings.Contains(msg, "note: six 1.17.0") || !strings.Contains(msg, "yanked") || !strings.Contains(msg, "broken metadata") {
		t.Errorf("stderr = %q; want a note naming six 1.17.0, yanked, and PyPI's reason", msg)
	}
	if lock := read(t, dir, "python/requirements.lock"); !strings.Contains(lock, "# pinned by upstream; yanked on PyPI: broken metadata\nsix==1.17.0 \\\n") {
		t.Errorf("requirements.lock =\n%s\nwant six's comment to say it is yanked, and why", lock)
	}

	src, dir = servers(t, "cryptography"), repo(t)
	stdout.Reset()
	stderr.Reset()
	if code := run(t.Context(), []string{"-C", dir}, src, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "cryptography 50.0.2 is yanked") {
		t.Errorf("run() with pins.txt's cryptography yanked = %d, stderr %q; want 1, naming it", code, stderr.String())
	}
}

func TestUsage(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	if code := run(t.Context(), []string{"extra"}, pins.Sources{}, &stdout, &stderr); code != 2 {
		t.Errorf("run(extra) = %d, want 2", code)
	}
}
