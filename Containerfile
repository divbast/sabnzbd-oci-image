# SPDX-License-Identifier: GPL-3.0-only
# SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

# SABnzbd, at a tag on its develop branch, pre-release or stable. Renovate
# moves these two in one PR, from SABnzbd's GitHub releases, and its regex
# needs them adjacent and in this order. The version tags the image
# (app-publish.yml reads it from this line, by the name build.yml gives as
# version-arg), and the probe compares it with the version SABnzbd
# reports. The commit is what the tag names: the fetch stage checks out
# the commit, checks its signature, and checks the tag still names it.
# go run ./cmd/divbast-pins reads the commit too, to lock the Python
# packages SABnzbd's requirements.txt there asks for.
ARG SABNZBD_VERSION=5.2.0RC2
ARG SABNZBD_COMMIT=ffc619947b5ba4e6520e3a0bc70680024e184b2f
# unrar's source, as RARLAB names its tarball, which is a counter, not
# unrar's version: the version is in the source's version.hpp. Renovate
# moves the tarball's name from the newest RARLAB's page links, betas
# included, and go run ./cmd/divbast-pins writes the sha256 of the tarball
# RARLAB served then, which publishes no checksum of its own. ADD holds the
# download to it.
ARG UNRAR_VERSION=7.3.2
ARG UNRAR_SHA256=f61a1160d6e2ebbda2ad353c8e82f62ab4312f97fde23c6c2c982d25e0014f99
# 7-Zip, from its own GitHub repository: Renovate moves the release and the
# commit its tag names in one PR, adjacent and in this order. The 7zz stage
# fetches the commit and checks the tag still names it.
ARG SEVENZIP_VERSION=26.06
ARG SEVENZIP_COMMIT=d3296e39e1f1f92c7b41c0c3848e65640428d42d
# The runtime: Docker Hardened Images' Python, with no shell, its nonroot
# user 65532, and pip, setuptools and wheel, which DHI ships in it and
# nothing here runs. SABnzbd runs on its Python, from a venv built by the
# -dev image's Python of the same release: Renovate moves the two in one
# PR. Its version is also the Python the locks are generated for. CI
# verifies its signature by Docker before anything is built, as it does
# every stage's image below.
ARG RUNTIME_IMAGE=dhi.io/python:3.15.2-debian13@sha256:d5387cad673412ef60e7fdadec757bd653c7dc289bfdccb4beff6d1ccec94f36

FROM ${RUNTIME_IMAGE} AS runtime

# The tools every build stage below uses, from DHI's own repository alone:
# Debian's sources are deleted first, so apt takes nothing DHI does not
# build, and fails on anything it lacks rather than falling back. Each
# package is pinned, one name=version a line, which Renovate's manager
# reads, to DHI's build. DHI deletes a superseded build, so a pin can fail
# the build until Renovate's PR moves it. Python's headers come with the
# image.
FROM dhi.io/python:3.15.2-debian13-dev@sha256:a1069444b00f18a15f0088a7f6c33ed6070d7f2593919130892b22c5a4f124b7 AS toolchain
SHELL ["/bin/bash", "-euo", "pipefail", "-c"]
RUN rm /etc/apt/sources.list.d/debian.sources \
    && apt-get update \
    && apt-get install -y --no-install-recommends \
        binutils=2.44-3+deb13u0+dhi10 \
        cargo=1.100.0-4 \
        curl=8.14.1-2+deb13u7+dhi1 \
        g++=4:14.2.0-1+deb13u0+dhi0 \
        gcc=4:14.2.0-1+deb13u0+dhi0 \
        git=1:2.47.3-0+deb13u1+dhi3 \
        gpg=2.4.7-21+deb13u1+dhi5 \
        gpg-agent=2.4.7-21+deb13u1+dhi5 \
        gzip=1.13-1+deb13u1+dhi1 \
        libffi-dev=3.4.8-2+deb13u0+dhi2 \
        libssl-dev=3.5.9-1~deb13u1+dhi2 \
        make=4.4.1-2+deb13u0+dhi1 \
        pkgconf=1.8.1-4+deb13u0+dhi1 \
        rustc=1.100.0-4 \
    && rm -rf /var/lib/apt/lists/*
# The check each native binary below must pass: position-independent, bound
# at load, with a read-only relocation table, and needing no library the
# runtime image does not ship, read from that image's own tree.
COPY --from=runtime /usr/lib /runtime/usr/lib
RUN install -d /usr/local/bin && cat >/usr/local/bin/check-elf <<'EOF' && chmod 0755 /usr/local/bin/check-elf
#!/bin/bash
set -euo pipefail
bin=$1
readelf -h "${bin}" | grep -q 'Type: *DYN' || { echo "ERROR: ${bin} is not position-independent" >&2; exit 1; }
readelf -d "${bin}" | grep -Eq 'BIND_NOW|FLAGS_1.* NOW' || { echo "ERROR: ${bin} is not bound at load (BIND_NOW)" >&2; exit 1; }
readelf -lW "${bin}" | grep -q GNU_RELRO || { echo "ERROR: ${bin} has no RELRO segment" >&2; exit 1; }
for lib in $(readelf -d "${bin}" | sed -n 's/.*(NEEDED).*\[\(.*\)\]/\1/p'); do
    if ! find /runtime -name "${lib}" -print -quit | grep -q .; then
        echo "ERROR: ${bin} needs ${lib}, which the runtime image does not ship" >&2
        exit 1
    fi
    echo "${bin} needs ${lib}"
done
EOF

# SABnzbd at its commit, fetched by commit, which git checks against its
# hash. Then its signature: SABnzbd's release commits are signed, and only
# by the key in keys/sabnzbd-release.asc, which is the one key in the
# keyring, so a commit unsigned or signed by any other key fails. The tag
# is asked for separately, peeled if it is ever annotated, so a tag moved
# upstream fails the build instead of shipping a commit no release names.
# The fetch and the tag lookup each get three retries, 10s, 30s and 90s
# apart, so one dropped connection does not cost the daily slot. .git goes
# once checked.
FROM toolchain AS fetch
ARG SABNZBD_VERSION
ARG SABNZBD_COMMIT
COPY keys/sabnzbd-release.asc /tmp/sabnzbd-release.asc
SHELL ["/bin/bash", "-euo", "pipefail", "-c"]
RUN retry() { \
        for delay in 10 30 90; do "$@" && return 0; sleep "${delay}"; done; \
        "$@"; \
    }; \
    key=ECDD582AECE8EA0886B07079755ABC43DD56C243; \
    git init -q /src; \
    git -C /src remote add origin https://github.com/sabnzbd/sabnzbd.git; \
    retry git -C /src fetch -q --depth 1 origin "${SABNZBD_COMMIT}"; \
    git -C /src checkout -q --detach FETCH_HEAD; \
    head=$(git -C /src rev-parse HEAD); \
    test "${head}" = "${SABNZBD_COMMIT}"; \
    export GNUPGHOME=/tmp/gnupg; \
    install -d -m 0700 "${GNUPGHOME}"; \
    gpg --batch --quiet --import /tmp/sabnzbd-release.asc; \
    echo "${key}:6:" | gpg --batch --quiet --import-ownertrust; \
    signed=$(git -C /src log -1 --format='%G? %GF %GP' HEAD); \
    read -r status signer primary <<<"${signed} - -"; \
    if [[ "${status}" != G || ( "${signer}" != "${key}" && "${primary}" != "${key}" ) ]]; then \
        echo "ERROR: SABnzbd commit ${SABNZBD_COMMIT} is not signed by SABnzbd's release key ${key}: git reads its signature as ${status}, by key ${signer}." >&2; \
        echo "If SABnzbd's release key changed, check the new key and replace keys/sabnzbd-release.asc and the fingerprint above." >&2; \
        exit 1; \
    fi; \
    retry git -C /src ls-remote origin \
        "refs/tags/${SABNZBD_VERSION}" "refs/tags/${SABNZBD_VERSION}^{}" >/tmp/tag; \
    tagged=$(awk -v tag="refs/tags/${SABNZBD_VERSION}" \
        '$2 == tag "^{}" { peeled = $1 } $2 == tag { plain = $1 } END { print (peeled != "" ? peeled : plain) }' /tmp/tag); \
    if [[ "${tagged}" != "${SABNZBD_COMMIT}" ]]; then \
        echo "tag ${SABNZBD_VERSION} names '${tagged}', the Containerfile pins ${SABNZBD_COMMIT}" >&2; \
        exit 1; \
    fi; \
    gpgconf --kill gpg-agent; \
    rm -rf /src/.git "${GNUPGHOME}" /tmp/tag
# The translations, compiled as upstream's own packaging compiles them, with
# the msgfmt.py SABnzbd carries; then the app tree, as upstream's snap
# ships it, without what only builds the Windows, macOS and snap packages,
# the tests, or the translations' sources.
WORKDIR /src
RUN python3 tools/make_mo.py >/tmp/make_mo.log \
    && mo=$(find locale -name SABnzbd.mo | wc -l) \
    && if ((mo < 10)); then echo "ERROR: make_mo.py wrote ${mo} translations" >&2; cat /tmp/make_mo.log >&2; exit 1; fi \
    && echo "${mo} translations" \
    && rm -rf tools/__pycache__ .github builder macos po snap tests win portable.cmd \
    && install -d /app \
    && cp -a /src /app/sabnzbd

# The Python packages, from requirements.lock: the pure ones from their
# wheels, and those with C or Rust code built from source here, against
# DHI's OpenSSL and libraries. Every file is first fetched by the URL and
# checked against the sha256 files.txt names, with curl and sha256sum, so
# no package's code runs while the network is on: pip download would run
# each source release's build backend to read its metadata. The Rust
# crates of each source release that carries a Cargo.lock, cryptography's
# and orjson's, are fetched with it, by cargo, which checks each against
# that Cargo.lock.
FROM toolchain AS deps
COPY python/files.txt python/requirements.lock python/build.lock /src/python/
SHELL ["/bin/bash", "-euo", "pipefail", "-c"]
RUN retry() { \
        for delay in 10 30 90; do "$@" && return 0; sleep "${delay}"; done; \
        "$@"; \
    }; \
    install -d /dl; \
    grep -v '^#' /src/python/files.txt | while read -r sha url; do \
        retry curl -fsSL --proto '=https' --tlsv1.2 -o "/dl/${url##*/}" "${url}"; \
        echo "${sha}  /dl/${url##*/}" | sha256sum -c --quiet -; \
    done; \
    export CARGO_HOME=/cargo; \
    for sdist in /dl/*.tar.gz; do \
        listing=$(tar -tzf "${sdist}"); \
        top=${listing%%/*}; \
        if grep -qx "${top}/Cargo.lock" <<<"${listing}"; then \
            tar -xzf "${sdist}" -C /tmp "${top}/Cargo.toml" "${top}/Cargo.lock"; \
            tar -xzf "${sdist}" -C /tmp --wildcards "${top}/src/*" 2>/dev/null || true; \
            retry cargo fetch --locked --manifest-path "/tmp/${top}/Cargo.toml"; \
            rm -rf "/tmp/${top:?}"; \
        fi; \
    done
# The build backends, from build.lock, into a venv of their own, which
# never ships. Then, with no network at all, every source release into a
# wheel, cffi and the pycparser it needs first, since cryptography's build
# calls on cffi. --check-build-dependencies holds each build to what its
# pyproject.toml asks for, from that venv, whose bin/ comes first on PATH,
# since maturin and cmake are run as programs.
RUN --network=none python3 -m venv /opt/build \
    && /opt/build/bin/pip install --no-index --find-links /dl --no-deps --require-hashes -r /src/python/build.lock \
    && /opt/build/bin/pip wheel --no-index --no-deps --no-build-isolation --check-build-dependencies \
        -w /wheels /dl/cffi-*.tar.gz \
    && /opt/build/bin/pip install --no-index --no-deps /wheels/cffi-*.whl /dl/pycparser-*.whl
RUN --network=none PATH="/opt/build/bin:${PATH}" CARGO_HOME=/cargo CARGO_NET_OFFLINE=true OPENSSL_NO_VENDOR=1 \
    /opt/build/bin/pip wheel --no-index --find-links /dl --no-deps --no-build-isolation --check-build-dependencies \
        --require-hashes -w /wheels -r /src/python/requirements.lock
# The app's venv, at the path it has in the runtime, with no pip of its
# own: every wheel installed into it by the build venv's pip, then pip's
# own check that each package's requirements are met there.
RUN --network=none python3 -m venv --without-pip /app/venv \
    && /opt/build/bin/pip --python /app/venv/bin/python install --no-index --no-deps /wheels/*.whl \
    && /opt/build/bin/pip --python /app/venv/bin/python check \
    && site=$(/app/venv/bin/python -c 'import sysconfig; print(sysconfig.get_path("purelib"))') \
    && for tool in pip setuptools wheel; do \
        if [[ -e "${site}/${tool}" ]]; then echo "ERROR: the venv holds ${tool}" >&2; exit 1; fi; \
    done
# Bytecode for -OO, compiled now since the root is read-only at run time.
# A hash-based .pyc that is never checked against its source needs no
# timestamp, so the same sources give the same files.
COPY --from=fetch /app/sabnzbd /app/sabnzbd
RUN --network=none python3 -m compileall -q -j0 -o 2 --invalidation-mode unchecked-hash /app/venv/lib /app/sabnzbd

# unrar, RARLAB's own, which SABnzbd needs to unpack RAR archives: from the
# tarball, held to its sha256, linked to the runtime's glibc and libstdc++,
# so the scan and DHI's updates cover them, with a stack protector,
# FORTIFY_SOURCE, PIE and full RELRO. upstream's -std=c++11 stays, since
# setting CXXFLAGS replaces its flags. Its version, from version.hpp, goes
# to the SBOM.
FROM toolchain AS unrar
ARG UNRAR_VERSION
ARG UNRAR_SHA256
ADD --checksum=sha256:${UNRAR_SHA256} https://www.rarlab.com/rar/unrarsrc-${UNRAR_VERSION}.tar.gz /tmp/unrar.tar.gz
SHELL ["/bin/bash", "-euo", "pipefail", "-c"]
RUN tar -xzf /tmp/unrar.tar.gz -C /tmp \
    && make -C /tmp/unrar -j"$(nproc)" unrar \
        CXXFLAGS="-O2 -std=c++11 -D_FORTIFY_SOURCE=3 -fstack-protector-strong -fPIE" \
        LDFLAGS="-pthread -pie -Wl,-z,relro,-z,now" \
    && install -D -m 0755 /tmp/unrar/unrar /out/unrar \
    && install -D -m 0644 /tmp/unrar/license.txt /out/doc/license.txt \
    && check-elf /out/unrar \
    && { /out/unrar || true; } >/tmp/banner \
    && grep -m1 'Alexander Roshal' /tmp/banner \
    && read -r major minor beta < <(awk '/#define RARVER_MAJOR/ {M=$3} /#define RARVER_MINOR/ {m=$3} /#define RARVER_BETA/ {b=$3} END {print M, m, b}' /tmp/unrar/version.hpp) \
    && if ((major < 5 || (major == 5 && minor < 50))); then echo "ERROR: unrar ${major}.${minor} is older than SABnzbd's 5.50" >&2; exit 1; fi \
    && printf '%s %s %s\n' "${major}" "${minor}" "${beta}" >/out/unrar.version

# 7zz, 7-Zip's own console program, from its GitHub repository by commit,
# with the tag checked as SABnzbd's is. No RAR codec: unrar does that.
# The makefiles take no CFLAGS, so the same hardening goes in MY_ARCH,
# which every compile and the link read, and the link's own in LDFLAGS.
# There is no assembler, so 7-Zip's C code stands in for its hand-written
# assembly.
FROM toolchain AS sevenzip
ARG SEVENZIP_VERSION
ARG SEVENZIP_COMMIT
SHELL ["/bin/bash", "-euo", "pipefail", "-c"]
RUN retry() { \
        for delay in 10 30 90; do "$@" && return 0; sleep "${delay}"; done; \
        "$@"; \
    }; \
    git init -q /src; \
    git -C /src remote add origin https://github.com/ip7z/7zip.git; \
    retry git -C /src fetch -q --depth 1 origin "${SEVENZIP_COMMIT}"; \
    git -C /src checkout -q --detach FETCH_HEAD; \
    head=$(git -C /src rev-parse HEAD); \
    test "${head}" = "${SEVENZIP_COMMIT}"; \
    retry git -C /src ls-remote origin \
        "refs/tags/${SEVENZIP_VERSION}" "refs/tags/${SEVENZIP_VERSION}^{}" >/tmp/tag; \
    tagged=$(awk -v tag="refs/tags/${SEVENZIP_VERSION}" \
        '$2 == tag "^{}" { peeled = $1 } $2 == tag { plain = $1 } END { print (peeled != "" ? peeled : plain) }' /tmp/tag); \
    if [[ "${tagged}" != "${SEVENZIP_COMMIT}" ]]; then \
        echo "tag ${SEVENZIP_VERSION} names '${tagged}', the Containerfile pins ${SEVENZIP_COMMIT}" >&2; \
        exit 1; \
    fi; \
    rm -rf /src/.git
WORKDIR /src/CPP/7zip/Bundles/Alone2
RUN make -j"$(nproc)" -f ../../cmpl_gcc.mak DISABLE_RAR=1 \
        MY_ARCH="-fPIE -fstack-protector-strong -D_FORTIFY_SOURCE=3" \
        LDFLAGS="-pie -Wl,-z,relro,-z,now" \
    && install -D -m 0755 b/g/7zz /out/7zz \
    && install -D -m 0644 -t /out/doc /src/DOC/License.txt /src/DOC/copying.txt \
    && check-elf /out/7zz \
    && /out/7zz i >/tmp/info \
    && if grep -i rar /tmp/info; then echo "ERROR: 7zz lists RAR, which DISABLE_RAR leaves out" >&2; exit 1; fi \
    && grep -m1 '7-Zip' /tmp/info

# The SBOM of what no scanner sees in the image: unrar and 7zz, built here,
# each by the identifier the scan matches it by. ci's divbast-scan reads
# it through the label below. unrar's version is its program's, from
# version.hpp, not the tarball's name.
FROM toolchain AS sbom
ARG SEVENZIP_VERSION
COPY --from=unrar /out/unrar.version /tmp/unrar.version
SHELL ["/bin/bash", "-euo", "pipefail", "-c"]
RUN read -r major minor beta </tmp/unrar.version \
    && unrar="${major}.${minor}" \
    && if ((beta > 0)); then version="${unrar} beta ${beta}"; else version="${unrar}"; fi \
    && install -d /out \
    && python3 - "${unrar}" "${version}" "${SEVENZIP_VERSION}" >/out/sbom.cdx.json <<'EOF'
import json, sys
unrar, unrar_version, sevenzip = sys.argv[1:]
json.dump({
    "bomFormat": "CycloneDX",
    "specVersion": "1.6",
    "version": 1,
    "components": [
        {
            "type": "application",
            "name": "unrar",
            "version": unrar_version,
            "cpe": f"cpe:2.3:a:rarlab:unrar:{unrar}:*:*:*:*:*:*:*",
            "licenses": [{"license": {"name": "UnRAR"}}],
            "properties": [{"name": "path", "value": "/usr/local/bin/unrar"}],
        },
        {
            "type": "application",
            "name": "7-zip",
            "version": sevenzip,
            "cpe": f"cpe:2.3:a:7-zip:7-zip:{sevenzip}:*:*:*:*:*:*:*",
            "licenses": [{"expression": "LGPL-2.1-or-later AND BSD-3-Clause AND BSD-2-Clause"}],
            "properties": [{"name": "path", "value": "/usr/local/bin/7zz"}],
        },
    ],
}, sys.stdout, indent=2)
print()
EOF

# The image's own command: it writes SABnzbd's first settings, then becomes
# SABnzbd, and is the HEALTHCHECK's probe. Static, on the standard library.
FROM dhi.io/golang:1.27.4-debian13-dev@sha256:121afb8b390b1d6babba8cd0a93eaf2ffa3735f835d388b9d79c2be38b41ee4f AS probe
ARG SABNZBD_VERSION
WORKDIR /src
# go.mod's replace names ./.ci, which the build context leaves out: the
# command imports nothing from divbast/ci, so Go never looks for it
COPY go.mod ./
COPY cmd/divbast-sabnzbd/ cmd/divbast-sabnzbd/
COPY internal/starter/ internal/starter/
COPY internal/probe/ internal/probe/
RUN CGO_ENABLED=0 GOFLAGS=-mod=readonly GOTOOLCHAIN=local \
    go build -trimpath -ldflags "-s -w -X main.version=${SABNZBD_VERSION}" \
    -o /out/divbast-sabnzbd ./cmd/divbast-sabnzbd \
    && install -d /out/config

FROM runtime
ARG SABNZBD_VERSION

# Root-owned, and never written: SABnzbd writes to /config and /tmp alone
COPY --from=deps /app/venv /app/venv
COPY --from=deps /app/sabnzbd /app/sabnzbd
COPY --from=unrar /out/unrar /usr/local/bin/unrar
COPY --from=sevenzip /out/7zz /usr/local/bin/7zz
# The licences unrar and 7-Zip ship under, beside the binaries' own notices
COPY --from=unrar /out/doc /usr/local/share/doc/unrar
COPY --from=sevenzip /out/doc /usr/local/share/doc/7zip
COPY --from=sbom /out/sbom.cdx.json /usr/local/share/divbast/sbom.cdx.json
COPY --from=probe /out/divbast-sabnzbd /usr/local/bin/divbast-sabnzbd
# SABnzbd's directory, empty and uid 65532's, and not a VOLUME: a new named
# volume mounted here takes its owner from it, so SABnzbd can write to it.
COPY --from=probe --chown=65532:65532 /out/config /config

# UTF-8 for SABnzbd and the tools it runs; no .pyc written at run time, on
# a read-only root, which the build compiled already; and each log line to
# the engine as it is written.
ENV LC_ALL=C.UTF-8 \
    PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1

# The licences of everything the image ships beyond its base: this
# repository's, SABnzbd's, each Python package's, unrar's and 7-Zip's. Which
# file carries which is in each package's dist-info, and in
# /usr/local/share/doc for the two binaries. The SBOM label is what ci's scan
# reads.
LABEL org.opencontainers.image.source="https://github.com/divbast/sabnzbd-oci-image" \
    org.opencontainers.image.url="https://github.com/divbast/sabnzbd-oci-image" \
    org.opencontainers.image.title="SABnzbd" \
    org.opencontainers.image.version="${SABNZBD_VERSION}" \
    org.opencontainers.image.licenses="GPL-3.0-only AND GPL-2.0-or-later AND GPL-2.0-only AND LGPL-3.0-or-later AND LGPL-2.1-or-later AND MPL-2.0 AND Apache-2.0 AND (EPL-2.0 OR BSD-3-Clause) AND BSD-3-Clause AND BSD-2-Clause AND MIT AND MIT-0 AND ISC AND PSF-2.0 AND TCL AND LicenseRef-UnRAR" \
    io.github.divbast.scan.sboms="/usr/local/share/divbast/sbom.cdx.json"

EXPOSE 8080
# Exec form: the shell form would need /bin/sh, which the image lacks.
# --timeout must clear divbast-sabnzbd health's own budget of 4 s for its
# one exchange. Podman reads no HEALTHCHECK from an image published as an
# OCI index: its users pass the same array as --health-cmd.
HEALTHCHECK --interval=30s --timeout=10s --start-period=60s --retries=3 \
    CMD ["/usr/local/bin/divbast-sabnzbd", "health"]
USER 65532:65532
# The command writes the first settings when /config has none, then
# replaces itself with SABnzbd, which is then PID 1 and handles its own
# signals.
ENTRYPOINT ["/usr/local/bin/divbast-sabnzbd", "serve"]
