// SPDX-License-Identifier: GPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestServeExecs checks serve writes the starter file, then execs SABnzbd
// with serverArgv and the environment it was given, unchanged.
func TestServeExecs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var gotPath string
	var gotArgv, gotEnv []string
	exec := func(path string, argv, envv []string) error {
		gotPath, gotArgv, gotEnv = path, argv, envv
		return errors.New("exec stopped by the test")
	}
	environ := []string{"PATH=/usr/bin", "SABNZBD_HOST_WHITELIST=sab", "LC_ALL=C.UTF-8"}
	var stdout bytes.Buffer
	if err := serve(dir, environ, exec, true, &stdout); err == nil || !strings.Contains(err.Error(), "exec stopped by the test") {
		t.Errorf("serve() = %v; want an error naming the failed exec", err)
	}
	want := serverArgv(true)
	if gotPath != want[0] || !slices.Equal(gotArgv, want) || !slices.Equal(gotEnv, environ) {
		t.Errorf("exec(%q, %q, %q), want exec(%q, %q, %q)", gotPath, gotArgv, gotEnv, want[0], want, environ)
	}
	data, err := os.ReadFile(filepath.Join(dir, "sabnzbd.ini")) //nolint:gosec // G304: the test's own temporary file
	if err != nil || !strings.Contains(string(data), "host_whitelist = sab\n") {
		t.Errorf("sabnzbd.ini = %q, %v; want the starter, with the whitelist", data, err)
	}
	if !strings.Contains(stdout.String(), "sabnzbd.ini") {
		t.Errorf("stdout = %q, want a line saying the starter file was written", stdout.String())
	}
}

// TestServeRefuses checks a bad whitelist stops serve before any exec.
func TestServeRefuses(t *testing.T) {
	t.Parallel()
	exec := func(string, []string, []string) error {
		t.Error("exec ran though the starter was refused")
		return nil
	}
	var stdout bytes.Buffer
	if err := serve(t.TempDir(), []string{"SABNZBD_HOST_WHITELIST=sab:8080"}, exec, true, &stdout); err == nil || !strings.Contains(err.Error(), "sab:8080") {
		t.Errorf("serve() = %v; want an error naming the entry", err)
	}
}

// TestServerArgv is SABnzbd's argv: on every address of both families,
// or IPv4's alone where the kernel has no IPv6.
func TestServerArgv(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		ipv6   bool
		listen string
	}{{true, ":::8080"}, {false, "0.0.0.0:8080"}} {
		want := []string{"/app/venv/bin/python", "-OO", "/app/sabnzbd/SABnzbd.py", "-f", "/config", "-s", tt.listen, "-b", "0", "--disable-file-log"}
		if got := serverArgv(tt.ipv6); !slices.Equal(got, want) {
			t.Errorf("serverArgv(%t) = %q, want %q", tt.ipv6, got, want)
		}
	}
}

// TestServeRefusesUnwritable is a config directory SABnzbd could not
// write: serve stops before any exec, naming it.
func TestServeRefusesUnwritable(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root writes past every mode")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sabnzbd.ini"), []byte("[misc]\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	exec := func(string, []string, []string) error {
		t.Error("exec ran though the settings file is read-only")
		return nil
	}
	var stdout bytes.Buffer
	if err := serve(dir, nil, exec, true, &stdout); err == nil || !strings.Contains(err.Error(), "sabnzbd.ini") {
		t.Errorf("serve() = %v; want an error naming the settings file", err)
	}
}
