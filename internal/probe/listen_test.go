// SPDX-License-Identifier: GPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package probe

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The heads of /proc/net/tcp and tcp6, and one socket line of each, as the
// kernel writes them: the local address and port in hex, then the remote,
// then the state, 0A for a listener and 01 for a connection.
const (
	tcpHead  = "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n"
	tcp6Head = "  sl  local_address                         remote_address                        st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n"
	anyV6    = "00000000000000000000000000000000"
	loV6     = "00000000000000000000000001000000"
)

// tcpListener is a socket listening on 8080, 1F90, as net/tcp lists it.
func tcpListener(addr string) string {
	return "   0: " + addr + ":1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000 65532        0 4242 1 0000000000000000 100 0 0 10 0\n"
}

func tcp6Line(addr, port, state string) string {
	return "   0: " + addr + ":" + port + " " + anyV6 + ":0000 " + state + " 00000000:00000000 00:00000000 00000000 65532        0 4242 1 0000000000000000 100 0 0 10 0\n"
}

// proc writes a /proc holding net/tcp and, unless tcp6 is nil, net/tcp6,
// each with the kernel's head line, and returns its path.
func proc(t *testing.T, tcp string, tcp6 *string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "net"), 0o750); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{"tcp": tcpHead + tcp}
	if tcp6 != nil {
		files["tcp6"] = tcp6Head + *tcp6
	}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, "net", name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestListening is where SABnzbd may listen: on every address, so the
// container's port reaches it, or not, as after a restored backup that
// names 127.0.0.1, when health must fail and say where it listens.
func TestListening(t *testing.T) {
	t.Parallel()
	const port = 8080 // 1F90
	for _, tt := range []struct {
		name      string
		tcp       string
		tcp6      *string
		wantError string
	}{
		{name: "every-address-v6", tcp6: new(tcp6Line(anyV6, "1F90", "0A"))},
		// A kernel with no IPv6, where the image listens on 0.0.0.0
		{name: "every-address-v4", tcp: tcpListener("00000000")},
		{name: "every-address-v4-and-v6-file", tcp: tcpListener("00000000"), tcp6: new("")},
		{name: "loopback-v4", tcp: tcpListener("0100007F"), tcp6: new(""), wantError: "127.0.0.1"},
		{name: "loopback-v6", tcp6: new(tcp6Line(loV6, "1F90", "0A")), wantError: "::1"},
		{name: "other-port", tcp6: new(tcp6Line(anyV6, "1F95", "0A")), wantError: "nothing listens on port 8080"},
		// A connection to 8080 is no listener
		{name: "connection-only", tcp6: new(tcp6Line(anyV6, "1F90", "01")), wantError: "nothing listens on port 8080"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := listening(proc(t, tt.tcp, tt.tcp6), port)
			switch {
			case tt.wantError == "" && err != nil:
				t.Errorf("listening() = %v, want nil", err)
			case tt.wantError != "" && (err == nil || !strings.Contains(err.Error(), tt.wantError)):
				t.Errorf("listening() = %v, want an error naming %q", err, tt.wantError)
			}
		})
	}
	if err := listening(t.TempDir(), port); err == nil {
		t.Error("listening() with no /proc/net/tcp = nil, want an error")
	}
}

// TestProbeListening is SABnzbd answering its version on loopback while it
// listens there alone: Probe fails with one "listen:" line, and passes once
// it listens on every address.
func TestProbeListening(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"version": "5.2.0RC2"}`))
	}))
	t.Cleanup(srv.Close)
	o := Options{Base: srv.URL, Version: "5.2.0RC2", Within: time.Second, Port: 8080}

	o.Proc = proc(t, tcpListener("0100007F"), new(""))
	var stderr bytes.Buffer
	if code := Probe(t.Context(), o, &stderr); code != 1 || !strings.HasPrefix(stderr.String(), "listen: ") || strings.Count(stderr.String(), "\n") != 1 {
		t.Errorf("Probe() listening on loopback alone = %d, %q; want 1 and one listen: line", code, stderr.String())
	}
	o.Proc = proc(t, "", new(tcp6Line(anyV6, "1F90", "0A")))
	stderr.Reset()
	if code := Probe(t.Context(), o, &stderr); code != 0 || stderr.Len() != 0 {
		t.Errorf("Probe() listening on every address = %d, %q; want 0 and nothing", code, stderr.String())
	}
}
