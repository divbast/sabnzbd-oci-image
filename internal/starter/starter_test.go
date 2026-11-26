// SPDX-License-Identifier: GPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package starter

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestParseWhitelist(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		in      string
		want    []string
		wantErr string
	}{
		{in: "", want: nil},
		{in: " , ,", want: nil},
		{in: " Sab.Example.com, sab ,", want: []string{"sab.example.com", "sab"}},
		{in: "[fd00::1],10.0.0.5", want: []string{"fd00::1", "10.0.0.5"}},
		{in: "FD00:0:0::1", want: []string{"fd00::1"}},
		{in: "sab-1.ns.svc.cluster.local", want: []string{"sab-1.ns.svc.cluster.local"}},
		{in: "sab,sab", want: []string{"sab"}},
		{in: "sab:8080", wantErr: `"sab:8080"`},
		{in: "[fd00::1]:8080", wantErr: `"[fd00::1]:8080"`},
		{in: "http://sab", wantErr: `"http://sab"`},
		{in: "*.example.com", wantErr: `"*.example.com"`},
		{in: "sab/x", wantErr: `"sab/x"`},
		{in: "sab,-bad-", wantErr: `"-bad-"`},
		{in: "sab example", wantErr: `"sab example"`},
		{in: "sab.example.com.", wantErr: `"sab.example.com."`},
	} {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()
			got, err := ParseWhitelist(tt.in)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) || !strings.Contains(err.Error(), Variable) {
					t.Errorf("ParseWhitelist(%q) = %q, %v; want an error naming %s and %s", tt.in, got, err, Variable, tt.wantErr)
				}
				return
			}
			if err != nil || !slices.Equal(got, tt.want) {
				t.Errorf("ParseWhitelist(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
			}
		})
	}
}

// env is an environment holding SABNZBD_HOST_WHITELIST as value, when not "".
func env(value string) func(string) string {
	return func(key string) string {
		if key == Variable {
			return value
		}
		return ""
	}
}

func TestEnsureWritesStarter(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ whitelist, want string }{
		{"", "[misc]\nenable_broadcast = 0\nfixed_ports = 1\n"},
		{"sab, Sab.Example.com", "[misc]\nenable_broadcast = 0\nfixed_ports = 1\nhost_whitelist = sab, sab.example.com\n"},
	} {
		t.Run(tt.whitelist, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			wrote, err := Ensure(dir, env(tt.whitelist))
			if err != nil || !wrote {
				t.Fatalf("Ensure() = %t, %v; want it written", wrote, err)
			}
			path := filepath.Join(dir, File)
			data, err := os.ReadFile(path) //nolint:gosec // G304: the test's own temporary file
			if err != nil || string(data) != tt.want {
				t.Errorf("%s =\n%s, %v\nwant\n%s", File, data, err, tt.want)
			}
			if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
				t.Errorf("%s's mode = %v, %v; want 0600", File, fi.Mode().Perm(), err)
			}
			// Nothing else: the temporary file is gone
			if entries, _ := os.ReadDir(dir); len(entries) != 1 {
				t.Errorf("the directory holds %d entries, want %s alone", len(entries), File)
			}
		})
	}
}

func TestEnsureLeavesExisting(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, File)
	const theirs = "[misc]\nhost = 0.0.0.0\nport = 8085\nhost_whitelist = mine,\n"
	if err := os.WriteFile(path, []byte(theirs), 0o600); err != nil {
		t.Fatal(err)
	}
	// Unreadable: Ensure must not need to read it
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	wrote, err := Ensure(dir, env("other"))
	if err != nil || wrote {
		t.Errorf("Ensure() over an existing file = %t, %v; want false, nil", wrote, err)
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o000 {
		t.Errorf("its mode = %v, %v; want 0000 still", fi.Mode().Perm(), err)
	}
	if err = os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != theirs { //nolint:gosec // G304: the test's own temporary file
		t.Errorf("it holds %q, %v; want it unchanged", data, err)
	}
}

func TestEnsureRefusesBadWhitelist(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	wrote, err := Ensure(dir, env("sab, sab:8080"))
	if err == nil || wrote || !strings.Contains(err.Error(), `"sab:8080"`) {
		t.Errorf("Ensure() = %t, %v; want an error naming the entry", wrote, err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("the directory holds %d entries after a refusal, want none", len(entries))
	}
}

func TestEnsureUnwritableDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil { //nolint:gosec // G302: a directory the test makes read-only
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) }) //nolint:gosec // G302: so the test's cleanup can remove it
	wrote, err := Ensure(dir, env(""))
	if err == nil || wrote || !strings.Contains(err.Error(), dir) {
		t.Errorf("Ensure() in a read-only directory = %t, %v; want an error naming %s", wrote, err, dir)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("the directory holds %d entries, want none", len(entries))
	}
}
