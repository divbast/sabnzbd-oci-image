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
ARG UNRAR_VERSION=7.3.2
ARG UNRAR_SHA256=f61a1160d6e2ebbda2ad353c8e82f62ab4312f97fde23c6c2c982d25e0014f99
ARG RUNTIME_IMAGE=dhi.io/python:3.15.2-debian13@sha256:c6c9414d78bd285be1a4dacfb94ae61732c77019605b1678792823e319b720f6

FROM dhi.io/python:3.15.2-debian13-dev@sha256:c73e2cb9e8d56cfab7f01b99f67b2c830e84e7397dfe666d8bfaf5a69ad0ce85 AS deps
FROM ${RUNTIME_IMAGE}
`

func TestReadArgs(t *testing.T) {
	t.Parallel()
	got, err := ReadArgs([]byte(containerfile))
	want := Args{
		SABnzbdCommit: "ffc619947b5ba4e6520e3a0bc70680024e184b2f", PythonVersion: "3.15",
		UnrarVersion: "7.3.2", UnrarSHA256: "f61a1160d6e2ebbda2ad353c8e82f62ab4312f97fde23c6c2c982d25e0014f99",
	}
	if err != nil || got != want {
		t.Errorf("ReadArgs() = %+v, %v; want %+v", got, err, want)
	}
	for _, tt := range []struct{ name, text, want string }{
		{"no-commit", strings.Replace(containerfile, "ARG SABNZBD_COMMIT=", "ARG SABNZBD_REF=", 1), "SABNZBD_COMMIT"},
		{"short-commit", strings.Replace(containerfile, "=ffc619947b5ba4e6520e3a0bc70680024e184b2f", "=ffc6199", 1), "SABNZBD_COMMIT"},
		{"twice", containerfile + "ARG UNRAR_VERSION=7.3.1\n", "UNRAR_VERSION"},
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

func TestSetArg(t *testing.T) {
	t.Parallel()
	sha := strings.Repeat("b", 64)
	got, err := SetArg([]byte(containerfile), "UNRAR_SHA256", sha)
	want := strings.Replace(containerfile, "f61a1160d6e2ebbda2ad353c8e82f62ab4312f97fde23c6c2c982d25e0014f99", sha, 1)
	if err != nil || string(got) != want {
		t.Errorf("SetArg() =\n%s, %v\nwant\n%s", got, err, want)
	}
	if _, err := SetArg([]byte(containerfile), "UNRAR_MD5", sha); err == nil || !strings.Contains(err.Error(), "UNRAR_MD5") {
		t.Errorf("SetArg() of an ARG the file lacks = %v, want an error naming it", err)
	}
}
