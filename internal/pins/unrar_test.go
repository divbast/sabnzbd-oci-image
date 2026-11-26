// SPDX-License-Identifier: GPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package pins

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUnrarChecksum(t *testing.T) {
	t.Parallel()
	tarball := []byte("unrar source, as RARLAB serves it")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rar/unrarsrc-7.3.2.tar.gz" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(tarball)
	}))
	t.Cleanup(srv.Close)
	h := sha256.Sum256(tarball)

	got, err := UnrarSHA256(t.Context(), srv.Client(), srv.URL, "7.3.2")
	if err != nil || got != hex.EncodeToString(h[:]) {
		t.Errorf("UnrarSHA256(7.3.2) = %s, %v; want %x", got, err, h)
	}
	if got, err = UnrarSHA256(t.Context(), srv.Client(), srv.URL, "7.3.3"); err == nil || !strings.Contains(err.Error(), "unrarsrc-7.3.3.tar.gz") || !strings.Contains(err.Error(), "404") {
		t.Errorf("UnrarSHA256(7.3.3) = %s, %v; want an error naming the tarball and the 404", got, err)
	}
}
