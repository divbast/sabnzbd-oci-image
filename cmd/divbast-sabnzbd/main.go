// SPDX-License-Identifier: GPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

// Command divbast-sabnzbd starts SABnzbd, and is the image's health check
// and version.
//
// Usage:
//
//	divbast-sabnzbd serve    write the first settings if /config has none, then become SABnzbd
//	divbast-sabnzbd health   check SABnzbd answers, and is the version the image was built from
//	divbast-sabnzbd version  print the version
//
// serve writes /config/sabnzbd.ini only when /config holds none: SSDP off,
// the port fixed at 8080, and host_whitelist from SABNZBD_HOST_WHITELIST
// when that is set. Then it replaces itself with SABnzbd, by execve, so
// SABnzbd is PID 1, and handles its own signals.
//
// health prints nothing when SABnzbd answers GET /api?mode=version with
// the image's version, and one "version: <reason>" line when it does not.
//
// Exit codes:
//
//	0  healthy, or the version printed
//	1  the first settings could not be written, SABnzbd could not be
//	   started, or health did not hold
//	2  the command line is wrong
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/divbast/sabnzbd-oci-image/internal/probe"
	"github.com/divbast/sabnzbd-oci-image/internal/starter"
)

// version is set at build time, with -ldflags "-X main.version=…": the
// SABnzbd version the image is built from.
var version = "dev"

// configDir is where SABnzbd keeps all its state.
const configDir = "/config"

// serverArgv is SABnzbd, as PID 1 runs it: from the venv's Python, at
// optimisation level 2 for the bytecode the build compiled, with its
// state in /config, on 8080, with no browser, and logging to stdout alone.
// It listens on ::, which takes IPv4 too, as IPv4-mapped addresses that
// SABnzbd reads as IPv4, so an IPv6-only network reaches it; a kernel with
// no IPv6 at all, ipv6, false, cannot open that socket, and gets 0.0.0.0.
// The host is written without brackets: SABnzbd saves it in its settings
// only if it is a plain address, and a restart from its web UI drops -s
// and listens where the settings say.
func serverArgv(ipv6 bool) []string {
	listen := ":::8080"
	if !ipv6 {
		listen = "0.0.0.0:8080"
	}
	return []string{
		"/app/venv/bin/python", "-OO", "/app/sabnzbd/SABnzbd.py",
		"-f", configDir, "-s", listen, "-b", "0", "--disable-file-log",
	}
}

// hasIPv6 reports whether the kernel can open an IPv6 socket.
func hasIPv6() bool {
	fd, err := syscall.Socket(syscall.AF_INET6, syscall.SOCK_STREAM, 0)
	if err != nil {
		return false
	}
	_ = syscall.Close(fd) // a probe socket, never used
	return true
}

// within bounds health's one exchange, inside the HEALTHCHECK's timeout.
const within = 4 * time.Second

func main() {
	if len(os.Args) != 2 {
		usage()
	}
	switch os.Args[1] {
	case "serve":
		err := serve(configDir, os.Environ(), syscall.Exec, hasIPv6(), os.Stdout)
		complain(os.Stderr, "%v", err)
		os.Exit(1)
	case "health":
		os.Exit(probe.Probe(context.Background(), probe.Options{Base: "http://127.0.0.1:8080", Version: version, Within: within}, os.Stderr))
	case "version":
		fmt.Println("divbast-sabnzbd", version)
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: divbast-sabnzbd serve|health|version")
	os.Exit(2)
}

// serve writes dir's first settings when it has none, from environ, checks
// SABnzbd can write dir and its settings, then has exec replace this
// process with SABnzbd, listening as ipv6 says, with environ unchanged. It
// returns only when one of them failed, with why.
func serve(dir string, environ []string, exec func(path string, argv, envv []string) error, ipv6 bool, stdout io.Writer) error {
	env := func(key string) string {
		for _, e := range slices.Backward(environ) {
			if k, v, ok := strings.Cut(e, "="); ok && k == key {
				return v
			}
		}
		return ""
	}
	wrote, err := starter.Ensure(dir, env)
	if err != nil {
		return err
	}
	if err := starter.Writable(dir); err != nil {
		return err
	}
	if wrote {
		// A failed write to stdout leaves nowhere better to report it.
		_, _ = fmt.Fprintf(stdout, "divbast-sabnzbd: wrote the first settings to %s/%s: SSDP off, port 8080 fixed\n", dir, starter.File)
	}
	argv := serverArgv(ipv6)
	return fmt.Errorf("could not start SABnzbd, %s: %w", argv[0], exec(argv[0], argv, environ))
}

// complain writes one line to stderr, after the command's name.
func complain(stderr io.Writer, format string, args ...any) {
	// A failed write to stderr leaves nowhere better to report it.
	_, _ = fmt.Fprintln(stderr, "divbast-sabnzbd:", fmt.Sprintf(format, args...))
}
