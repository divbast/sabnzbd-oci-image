# SPDX-License-Identifier: GPL-3.0-only
# SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

# The app's own version, which tags the image: app-publish.yml reads it from
# this line, by the name build.yml gives as version-arg
ARG SABNZBD_VERSION=5.2.0RC2
# The commit SABNZBD_VERSION's tag names. go run ./cmd/divbast-pins reads it,
# to lock the Python packages SABnzbd's requirements.txt there asks for.
ARG SABNZBD_COMMIT=ffc619947b5ba4e6520e3a0bc70680024e184b2f
# unrar's source, as RARLAB names its tarball, and the sha256 of the one it
# served when the version was pinned, which go run ./cmd/divbast-pins writes
ARG UNRAR_VERSION=7.3.2
ARG UNRAR_SHA256=f61a1160d6e2ebbda2ad353c8e82f62ab4312f97fde23c6c2c982d25e0014f99
# 7-Zip's release, and the commit its tag names
ARG SEVENZIP_VERSION=26.06
ARG SEVENZIP_COMMIT=d3296e39e1f1f92c7b41c0c3848e65640428d42d
# The runtime: Docker Hardened Images' Python, which SABnzbd runs on. Its
# version is the Python the locks are generated for. CI verifies its
# signature by Docker before anything is built.
ARG RUNTIME_IMAGE=dhi.io/python:3.15.2-debian13@sha256:c6c9414d78bd285be1a4dacfb94ae61732c77019605b1678792823e319b720f6

# The newest stable Go, by digest. Only the binary leaves this stage.
FROM golang:1.27.4-trixie@sha256:430985542100b871e108ccc00e8dcc5eef08ca14ee5825e98ebf563d8ffec4ee AS build
ARG SABNZBD_VERSION
WORKDIR /src
# go.mod's replace names ./.ci, which the build context leaves out: the app
# imports nothing from divbast/ci, so Go never looks for it
COPY go.mod ./
COPY cmd/ cmd/
COPY internal/ internal/
RUN CGO_ENABLED=0 GOFLAGS=-mod=readonly GOTOOLCHAIN=local \
    go build -trimpath -ldflags="-s -w -X main.version=${SABNZBD_VERSION}" -o /out/divbast-sabnzbd ./cmd/divbast-sabnzbd

FROM ${RUNTIME_IMAGE}
ARG SABNZBD_VERSION

# Root-owned, and never written
COPY --from=build /out/divbast-sabnzbd /usr/local/bin/divbast-sabnzbd

LABEL org.opencontainers.image.source="https://github.com/divbast/sabnzbd-oci-image" \
    org.opencontainers.image.url="https://github.com/divbast/sabnzbd-oci-image" \
    org.opencontainers.image.title="sabnzbd" \
    org.opencontainers.image.version="${SABNZBD_VERSION}" \
    org.opencontainers.image.licenses="GPL-3.0-only"

EXPOSE 8080 8081
# Exec form: the shell form would need /bin/sh, which the image lacks.
# --timeout must clear divbast-sabnzbd health's own budget: it allows each of its
# two exchanges 4 s and runs them one after the other, so a listener that
# accepts and never answers costs 8 s. A shorter timeout kills the probe
# mid-flight and records a bare engine timeout instead of the probe's own
# line, which is the only diagnostic there is. Podman reads no HEALTHCHECK
# from an image published as an OCI index: its users pass the same array
# as --health-cmd.
HEALTHCHECK --interval=30s --timeout=10s --start-period=60s --retries=3 \
    CMD ["/usr/local/bin/divbast-sabnzbd", "health"]
USER 65532:65532
# The server itself, as PID 1: the image has no state to prepare and no
# environment to read, so nothing runs before it
ENTRYPOINT ["/usr/local/bin/divbast-sabnzbd", "serve"]
