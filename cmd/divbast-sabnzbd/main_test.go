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
	if err := serve(dir, environ, exec, &stdout); err == nil || !strings.Contains(err.Error(), "exec stopped by the test") {
		t.Errorf("serve() = %v; want an error naming the failed exec", err)
	}
	if gotPath != serverArgv[0] || !slices.Equal(gotArgv, serverArgv) || !slices.Equal(gotEnv, environ) {
		t.Errorf("exec(%q, %q, %q), want exec(%q, %q, %q)", gotPath, gotArgv, gotEnv, serverArgv[0], serverArgv, environ)
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
	if err := serve(t.TempDir(), []string{"SABNZBD_HOST_WHITELIST=sab:8080"}, exec, &stdout); err == nil || !strings.Contains(err.Error(), "sab:8080") {
		t.Errorf("serve() = %v; want an error naming the entry", err)
	}
}

func TestServerArgv(t *testing.T) {
	t.Parallel()
	want := []string{"/app/venv/bin/python", "-OO", "/app/sabnzbd/SABnzbd.py", "-f", "/config", "-s", "0.0.0.0:8080", "-b", "0", "--disable-file-log"}
	if !slices.Equal(serverArgv, want) {
		t.Errorf("serverArgv = %q, want %q", serverArgv, want)
	}
}
