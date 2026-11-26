// SPDX-License-Identifier: GPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

// Package probe is the image's health check: SABnzbd answers its version,
// it is the version the image was built from, and it listens on every
// address, so the container's port reaches it.
package probe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Options say where SABnzbd is, which version it must be, and how long
// it has to answer.
type Options struct {
	// Base is SABnzbd's address from inside the container,
	// http://127.0.0.1:8080: an IP address and loopback, which SABnzbd's
	// host and access checks always let in.
	Base    string
	Version string
	Within  time.Duration
	// Port, when not 0, is the port SABnzbd must listen on on every
	// address, as the network namespace that Proc, /proc, describes.
	Port int
	Proc string
}

// Probe asks SABnzbd for GET /api?mode=version, which needs no API key, and
// returns 0 when it answers 200 with exactly o.Version and, with o.Port
// set, listens on that port on every address, printing nothing. Otherwise
// it prints one "version: <reason>" or "listen: <reason>" line to stderr,
// which is the only record of why, and returns 1.
func Probe(ctx context.Context, o Options, stderr io.Writer) int {
	// A failed write to stderr leaves nowhere better to report it.
	if err := check(ctx, o); err != nil {
		_, _ = fmt.Fprintln(stderr, "version:", err)
		return 1
	}
	if o.Port == 0 {
		return 0
	}
	if err := listening(o.Proc, o.Port); err != nil {
		_, _ = fmt.Fprintln(stderr, "listen:", err)
		return 1
	}
	return 0
}

func check(ctx context.Context, o Options) error {
	ctx, cancel := context.WithTimeout(ctx, o.Within)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.Base+"/api?mode=version", http.NoBody)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }() // read-only, so nothing to lose
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET /api?mode=version answered %d, want 200", resp.StatusCode)
	}
	var got struct {
		Version *string `json:"version"`
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&got); err != nil {
		return fmt.Errorf("GET /api?mode=version answered no JSON: %w", err)
	}
	if got.Version == nil {
		return errors.New("GET /api?mode=version answered no version")
	}
	if *got.Version != o.Version {
		return fmt.Errorf("SABnzbd answered version %q, want %q", *got.Version, o.Version)
	}
	return nil
}
