// SPDX-License-Identifier: GPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package pins

import (
	"strings"
	"testing"
)

// containerfile holds the lines the tool reads, as the image's
// Containerfile writes them.
const containerfile = `# The app
ARG SABNZBD_VERSION=5.2.0RC2
ARG SABNZBD_COMMIT=ffc619947b5ba4e6520e3a0bc70680024e184b2f
ARG RUNTIME_IMAGE=dhi.io/python:3.15.2-debian13@sha256:d5387cad673412ef60e7fdadec757bd653c7dc289bfdccb4beff6d1ccec94f36

FROM dhi.io/python:3.15.2-debian13-dev@sha256:a1069444b00f18a15f0088a7f6c33ed6070d7f2593919130892b22c5a4f124b7 AS deps
FROM ${RUNTIME_IMAGE}
`

func TestReadArgs(t *testing.T) {
	t.Parallel()
	got, err := ReadArgs([]byte(containerfile))
	want := Args{
		SABnzbdCommit: "ffc619947b5ba4e6520e3a0bc70680024e184b2f", PythonVersion: "3.15", PythonFullVersion: "3.15.2",
	}
	if err != nil || got != want {
		t.Errorf("ReadArgs() = %+v, %v; want %+v", got, err, want)
	}
	for _, tt := range []struct{ name, text, want string }{
		{"no-commit", strings.Replace(containerfile, "ARG SABNZBD_COMMIT=", "ARG SABNZBD_REF=", 1), "SABNZBD_COMMIT"},
		{"short-commit", strings.Replace(containerfile, "=ffc619947b5ba4e6520e3a0bc70680024e184b2f", "=ffc6199", 1), "SABNZBD_COMMIT"},
		{"twice", containerfile + "ARG SABNZBD_COMMIT=ffc619947b5ba4e6520e3a0bc70680024e184b2f\n", "SABNZBD_COMMIT"},
		// The -dev image and the runtime must be one Python
		{"two-pythons", strings.Replace(containerfile, "3.15.2-debian13-dev", "3.14.10-debian13-dev", 1), "3.14.10"},
		{"no-python", strings.ReplaceAll(containerfile, "dhi.io/python:", "dhi.io/golang:"), "dhi.io/python"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := ReadArgs([]byte(tt.text)); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("ReadArgs() = %v, want an error naming %q", err, tt.want)
			}
		})
	}
}
