// SPDX-License-Identifier: GPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package acceptance

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/divbast/ci/harness"
)

// TestServes is a fresh install: / sends a browser to SABnzbd's setup
// wizard, and the version, which needs no API key, is the image's own.
func TestServes(t *testing.T) {
	t.Parallel()
	e, image := underTest(t)
	s, _ := serveFresh(t, e, image, nil)
	checks := harness.NewChecks(t)

	resp, err := s.Get(t.Context(), "/", nil)
	harness.Answers(t, "GET / on a fresh install redirects", http.StatusFound, resp, err)
	if err == nil {
		loc := resp.Header.Get("Location")
		checks.True("it redirects to the setup wizard", strings.HasSuffix(strings.TrimSuffix(loc, "/"), "/wizard"), loc)
	}

	resp, err = s.Get(t.Context(), versionPath, nil)
	harness.Answers(t, "GET "+versionPath+" answers 200, with no API key", http.StatusOK, resp, err)
	if err == nil {
		var v struct{ Version string }
		checks.True("it answers JSON", json.Unmarshal(resp.Body, &v) == nil, harness.Clip(string(resp.Body)))
		harness.Equal(checks, "it is the image's version", imageVersion(t), v.Version)
	}
	healthy(t, s, "the shipped probe passes")
}
