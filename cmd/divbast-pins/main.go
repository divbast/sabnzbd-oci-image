// SPDX-License-Identifier: GPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

// Command divbast-pins writes the image's Python locks and unrar's
// checksum from what the repository pins.
//
// Usage:
//
//	go run ./cmd/divbast-pins [-check] [-C DIR]
//
// It reads, in DIR, the repository's root by default:
//   - the Containerfile's SABNZBD_COMMIT, its dhi.io/python images, which
//     must be one Python, and UNRAR_VERSION;
//   - python/pins.txt, each pin under a comment saying why it is there.
//
// It fetches SABnzbd's requirements.txt at that commit from GitHub, each
// package's release from PyPI, and unrar's tarball from RARLAB, and
// writes:
//   - python/requirements.lock: every package the image installs, for
//     Linux and that Python, at upstream's own pin or else pins.txt's, with
//     the sha256 of its pure wheel or, for one with C or Rust code, of its
//     source release, which the build compiles;
//   - python/build.lock: the build backends pins.txt names, with the
//     sha256 of each wheel a build stage may install;
//   - the Containerfile's UNRAR_SHA256, the checksum of the tarball RARLAB
//     serves now.
//
// Renovate runs it in each pull request that moves one of those pins.
// With -check it writes nothing, prints a unified diff of each file that
// differs from what it would write, and exits 1 if any does, so CI holds
// the locks to the pins.
//
// Exit codes:
//
//	0  the files written, or, with -check, already current
//	1  a file is stale, with -check; or a pin, upstream's list, PyPI,
//	   GitHub or RARLAB refused what the files need, or a file could not
//	   be read or written
//	2  the command line is wrong
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"slices"
	"syscall"

	"github.com/divbast/sabnzbd-oci-image/internal/pins"
)

const name = "divbast-pins"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], pins.DefaultSources(), os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// run is the command, reading upstream from src.
func run(ctx context.Context, args []string, src pins.Sources, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(stderr)
	check := flags.Bool("check", false, "write nothing; print a diff of each stale file, and exit 1 if any is")
	dir := flags.String("C", ".", "the repository's root `DIR`")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() > 0 {
		complain(stderr, "unexpected argument %q", flags.Arg(0))
		flags.Usage()
		return 2
	}
	root, err := os.OpenRoot(*dir)
	if err != nil {
		complain(stderr, "%v", err)
		return 1
	}
	defer func() { _ = root.Close() }() // every write is checked where it is made
	files, err := pins.Generate(ctx, src, root)
	if err != nil {
		complain(stderr, "%v", err)
		return 1
	}
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	stale := 0
	for _, p := range paths {
		old, err := root.ReadFile(p)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			complain(stderr, "%v", err)
			return 1
		}
		if bytes.Equal(old, files[p]) {
			continue
		}
		if *check {
			stale++
			if _, err = io.WriteString(stdout, pins.Diff(p, old, files[p])); err != nil {
				complain(stderr, "write the diff: %v", err)
				return 1
			}
			continue
		}
		if err = root.WriteFile(p, files[p], 0o644); err != nil {
			complain(stderr, "%v", err)
			return 1
		}
		if _, err = fmt.Fprintf(stdout, "wrote %s\n", p); err != nil {
			complain(stderr, "write what was written: %v", err)
			return 1
		}
	}
	if stale > 0 {
		complain(stderr, "%d file(s) do not match the pins: run go run ./cmd/%s, and commit what it writes", stale, name)
		return 1
	}
	return 0
}

// complain writes one line to stderr, after the command's name.
func complain(stderr io.Writer, format string, args ...any) {
	// A failed write to stderr leaves nowhere better to report it.
	_, _ = fmt.Fprintln(stderr, name+":", fmt.Sprintf(format, args...))
}
