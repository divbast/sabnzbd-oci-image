// SPDX-License-Identifier: GPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package probe

import (
	"bytes"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHealth(t *testing.T) {
	t.Parallel()
	const within = 300 * time.Millisecond
	answer := func(status int, body string) string {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api" || r.URL.Query().Get("mode") != "version" {
				http.NotFound(w, r)
				return
			}
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}))
		t.Cleanup(srv.Close)
		return srv.URL
	}
	// A port nothing listens on
	closed := func() string {
		l, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addr := l.Addr().String()
		_ = l.Close()
		return "http://" + addr
	}
	// A listener that accepts and never answers
	silent := func() string {
		l, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = l.Close() })
		go func() {
			for {
				c, err := l.Accept()
				if err != nil {
					return
				}
				t.Cleanup(func() { _ = c.Close() })
			}
		}()
		return "http://" + l.Addr().String()
	}
	for _, tt := range []struct {
		name, base string
		wantCode   int
		want       string
	}{
		{"right-version", answer(200, `{"version": "5.2.0RC2"}`), 0, ""},
		{"wrong-version", answer(200, `{"version": "5.1.3"}`), 1, `version: SABnzbd answered version "5.1.3", want "5.2.0RC2"` + "\n"},
		{"no-json", answer(200, `<html>`), 1, "version: "},
		{"forbidden", answer(403, "Access denied"), 1, "version: GET /api?mode=version answered 403, want 200\n"},
		{"unavailable", answer(503, ""), 1, "version: GET /api?mode=version answered 503, want 200\n"},
		{"refused", closed(), 1, "version: "},
		{"silent", silent(), 1, "version: "},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stderr bytes.Buffer
			start := time.Now()
			code := Probe(t.Context(), Options{Base: tt.base, Version: "5.2.0RC2", Within: within}, &stderr)
			took := time.Since(start)
			if code != tt.wantCode {
				t.Errorf("Probe() = %d, want %d; stderr %q", code, tt.wantCode, stderr.String())
			}
			if tt.want == "" && stderr.Len() > 0 || !strings.HasPrefix(stderr.String(), tt.want) {
				t.Errorf("stderr = %q, want it to start %q", stderr.String(), tt.want)
			}
			if n := strings.Count(stderr.String(), "\n"); tt.wantCode == 1 && n != 1 {
				t.Errorf("stderr = %q, %d lines; want one", stderr.String(), n)
			}
			if took > within+time.Second {
				t.Errorf("Probe() took %v, past its budget of %v", took, within)
			}
		})
	}
}
