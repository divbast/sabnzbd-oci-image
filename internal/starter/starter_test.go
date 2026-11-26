// SPDX-License-Identifier: GPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package starter

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
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

func TestWritable(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root writes past every mode")
	}
	ok := t.TempDir()
	if err := Writable(ok); err != nil {
		t.Errorf("Writable() of a writable directory = %v", err)
	}
	if err := os.WriteFile(filepath.Join(ok, File), []byte("[misc]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Writable(ok); err != nil {
		t.Errorf("Writable() with a writable %s = %v", File, err)
	}
	if entries, _ := os.ReadDir(ok); len(entries) != 1 {
		t.Errorf("Writable() left %d entries, want %s alone", len(entries), File)
	}

	readOnly := t.TempDir()
	if err := os.Chmod(readOnly, 0o500); err != nil { //nolint:gosec // G302: a directory the test makes read-only
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(readOnly, 0o700) }) //nolint:gosec // G302: so the test's cleanup can remove it
	if err := Writable(readOnly); err == nil || !strings.Contains(err.Error(), readOnly) || !strings.Contains(err.Error(), "chown") {
		t.Errorf("Writable() of a read-only directory = %v, want an error naming it and saying chown", err)
	}

	// A directory SABnzbd can write, holding a settings file it cannot
	stale := t.TempDir()
	ini := filepath.Join(stale, File)
	if err := os.WriteFile(ini, []byte("[misc]\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	if err := Writable(stale); err == nil || !strings.Contains(err.Error(), ini) {
		t.Errorf("Writable() with a read-only %s = %v, want an error naming it", File, err)
	}
	if data, err := os.ReadFile(ini); err != nil || string(data) != "[misc]\n" { //nolint:gosec // G304: the test's own temporary file
		t.Errorf("%s holds %q, %v after Writable(); want it unchanged", File, data, err)
	}
}

// noLinks is a filesystem with no hard links, as SMB shares and some
// network volumes are: link fails as Linux's link(2) does there.
func noLinks(errno syscall.Errno) func(string, string) error {
	return func(oldname, newname string) error {
		return &os.LinkError{Op: "link", Old: oldname, New: newname, Err: errno}
	}
}

// TestEnsureWithoutHardLinks is /config on a filesystem that refuses hard
// links: the first settings are written all the same, created where no
// file is, and only there.
func TestEnsureWithoutHardLinks(t *testing.T) {
	t.Parallel()
	const want = "[misc]\nenable_broadcast = 0\nfixed_ports = 1\nhost_whitelist = sab\n"
	for _, errno := range []syscall.Errno{syscall.EPERM, syscall.EOPNOTSUPP} {
		t.Run(errno.Error(), func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			wrote, err := ensure(dir, env("sab"), noLinks(errno))
			if err != nil || !wrote {
				t.Fatalf("ensure() = %t, %v; want it written", wrote, err)
			}
			path := filepath.Join(dir, File)
			if data, err := os.ReadFile(path); err != nil || string(data) != want { //nolint:gosec // G304: the test's own temporary file
				t.Errorf("%s =\n%s, %v\nwant\n%s", File, data, err, want)
			}
			if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
				t.Errorf("%s's mode = %v, %v; want 0600", File, fi.Mode().Perm(), err)
			}
			if entries, _ := os.ReadDir(dir); len(entries) != 1 {
				t.Errorf("the directory holds %d entries, want %s alone", len(entries), File)
			}
		})
	}
	// A file that appears while the first settings are written is theirs
	dir := t.TempDir()
	const theirs = "[misc]\nport = 8085\n"
	appears := func(oldname, newname string) error {
		if err := os.WriteFile(newname, []byte(theirs), 0o600); err != nil {
			return err
		}
		return noLinks(syscall.EPERM)(oldname, newname)
	}
	if wrote, err := ensure(dir, env(""), appears); err != nil || wrote {
		t.Errorf("ensure() as a file appears = %t, %v; want false, nil", wrote, err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, File)); err != nil || string(data) != theirs { //nolint:gosec // G304: the test's own temporary file
		t.Errorf("%s = %q, %v; want the file that appeared, unchanged", File, data, err)
	}
	// Any other failure of the link is an error, and writes nothing
	dir = t.TempDir()
	if wrote, err := ensure(dir, env(""), noLinks(syscall.EIO)); err == nil || wrote {
		t.Errorf("ensure() with link failing EIO = %t, %v; want an error", wrote, err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("the directory holds %d entries after a failed link, want none", len(entries))
	}
}
