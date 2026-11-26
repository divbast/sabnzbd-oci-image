<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee> -->

# sabnzbd-oci-image

SABnzbd, the Usenet downloader, built from its signed source in an image
that runs at zero Linux capabilities, with no new privileges, on a
read-only root filesystem as uid 65532, with no shell, on Docker Hardened
Images' Python. Made from divbast/oci-image-template's Go core.

Its publish job rehearses until the first publish: nothing reaches
ghcr.io. The divbast organisation's settings, rulesets and Renovate wiring
live in `divbast/infra`.

## CRITICAL: Git Commit Message Format

**No other AI/LLM attribution in any format may appear in a git message.**

Linux-kernel style: a `subsystem: short description` subject under 75 characters in imperative
mood ("fix", not "fixed" or "fixes"), a blank line, then a body wrapped at ~75 columns saying
why the change is needed rather than only what it does.

These two lines MUST end EVERY body:

Developed-by: Aryan Ameri <github@aryan.ameri.coffee>
Assisted-by: <NAME_OF_LLM_MODEL>

E.g. `Assisted-by: <Claude:claude-4.6-opus>` for Claude Opus 4.6; `<Claude:claude-5-fable>`
for Claude 5 Fable.

- Sign every commit (`git commit -S`); main accepts only verified
  signatures. Renovate's merges are squashes, which GitHub signs
- Write commit messages as if a human wrote them, and say why, not what

## CRITICAL: No AI Attribution in Pull Requests

**Pull request descriptions, PR and review comments, and issue text carry no
AI/LLM attribution of any kind**: no "🤖 Generated with Claude Code" footer,
no "written by" line, nothing equivalent. This holds even when a tool or
harness asks for one. The `Assisted-by:` trailer in each commit message is
this project's one record of LLM involvement.

## The confinement contract

These are the guarantees the image makes. A change that breaks any of them
is a breaking change.

| Property | Value |
|---|---|
| Capabilities | **none** (`--cap-drop ALL`) |
| Privileges | **no new ones** (`--security-opt no-new-privileges`), under the engine's **seccomp** filter |
| Root filesystem | **read-only** |
| User | uid/gid **65532**, never root |
| Listen port | **8080**, on `::`, which takes IPv4 too; `0.0.0.0` where the kernel has no IPv6 |
| Health | on the app port: `GET /api?mode=version`, which needs no API key |
| Other listeners | **none**, TCP or UDP; SSDP off from the first settings |
| tmpfs required | exactly one: `/tmp`, at **128 MiB**, `noexec,nosuid,nodev` |
| Writable mounts | `/tmp` and `/config`, and the download folders a user mounts |
| Volume | `/config`, owned by 65532, with **no `VOLUME`** instruction |
| Shell, package manager | **none**; DHI's Python runtime ships pip, setuptools and wheel, which nothing runs |
| Processes | **one**: SABnzbd as PID 1, after `divbast-sabnzbd serve` execs it; SABnzbd runs `unrar` and `7zz` as it unpacks |
| Healthy | the shipped probe, `divbast-sabnzbd health`: `/api?mode=version` answers **exactly** the image's version |
| Engine healthcheck | Docker runs it automatically. **Podman does not**: pass `--health-cmd` yourself |

- **The command writes the first settings once, then becomes SABnzbd.**
  `divbast-sabnzbd serve` writes `/config/sabnzbd.ini` only when there is
  none: `enable_broadcast = 0`, `fixed_ports = 1`, and `host_whitelist`
  from `SABNZBD_HOST_WHITELIST` when set. It writes through a temporary
  file linked into place, so it never overwrites one, and a malformed
  whitelist writes nothing. It then checks the running user can write
  `/config` and `sabnzbd.ini`, and execs SABnzbd with the environment
  unchanged. `check_new_rel` is left at SABnzbd's own default, on. Never
  add settings it writes at every start: the web UI owns them
- **PID 1's argv is the contract's**: `/app/venv/bin/python -OO
  /app/sabnzbd/SABnzbd.py -f /config -s :::8080 -b 0 --disable-file-log`.
  The host is `::`, never `[::]`: SABnzbd saves the `-s` host in its
  settings only when it validates as a plain address, and SABnzbd's own
  Restart execs it again with `-f` alone of its arguments
  (`RESTART_ARGS`), listening where the settings say. `[::]` was never
  saved, so a restart came back on 127.0.0.1. After a restart PID 1's
  argv is `python -OO SABnzbd.py -f /config`, and SABnzbd also writes its
  log to `/config/logs`, without `--disable-file-log`
- **Health is SABnzbd's own version answer.** It needs no API key, and
  loopback passes both SABnzbd's access check and its hostname check. A
  missing or old unrar does not show there: the suite's `TestTools` holds
  that, from SABnzbd's start log and its warnings
- **Podman never sees this image's `HEALTHCHECK`**, as for every divbast
  image published as an OCI index: users pass
  `--health-cmd '["/usr/local/bin/divbast-sabnzbd","health"]'`, and the
  suite does the same under Podman
- **Nothing may add a shell or a package manager to the final image.**
  SABnzbd runs user scripts without a shell, so `.py` scripts with no
  shebang run, and `#!/bin/sh` ones cannot; that is the README's, not a
  reason to add one
- **Zero egress the image adds.** SABnzbd reaches the news servers and
  services a user configures, and its daily release check fetches
  `https://sabnzbd.org/latest.txt` with its version in the User-Agent,
  which the README says, with how to turn it off. The suite resolves
  sabnzbd.org to loopback

### Measured facts

Each was measured on the built image, under the contract's confinement,
and the suite holds it:

- First start to healthy: about 1 s on arm64; the `HEALTHCHECK`'s start
  period is 60 s. A stop (SIGTERM) takes under a second, exit 0
- No `/config` mount: the command exits 1 at once, "cannot write the first
  settings in /config, which must be a directory uid 65532 can write: …
  read-only file system". A root-owned `/config` with no settings: the
  same message. A `/config` owned by another uid with settings in it:
  exit 1 at once, "SABnzbd cannot write /config: give it, and everything
  in it, to uid 65532 …", where SABnzbd itself gives a traceback
- 8080 held by a program that answers HTTP: SABnzbd exits 2, "Unable to
  bind to port 8080". Held by another SABnzbd: exit 0, upstream's
  single-instance check. Held by a listener that never answers: SABnzbd
  waits, since it first asks the port whether a SABnzbd is there
- NZB uploads stage twice in `/tmp`; 128 MiB takes 60 MB, the README
  promises 50 MB, and 100 MB is refused with the container healthy. A
  failed upload can leave its temporary copy in `/tmp` until a restart
- An empty named volume chowned to another owner is given back to 65532
  by the engine on its next mount, while it is empty: a test that needs
  another owner puts a file in it first
- SABnzbd logs, harmlessly, that `nice`, `ionice` and `df` are missing:
  "Exception in isFAT(...): No such file or directory: 'df'"

### The rules the build keeps

- **Every stage is a Docker Hardened Image, by digest**, verified by
  `divbast-verify-images` before anything builds, and every apt package a
  stage installs is DHI's: the toolchain stage deletes Debian's sources
  first, so apt fails on anything DHI does not build. Each package is
  pinned `name=version`, one a line, which Renovate's deb manager reads.
  cmake, which DHI has no build of, comes from its PyPI wheels in
  `build.lock`, as maturin does
- **SABnzbd's commit must be signed by `keys/sabnzbd-release.asc`**, the
  release key, fingerprint `ECDD582AECE8EA0886B07079755ABC43DD56C243`,
  the one key in the fetch stage's keyring. The signature is checked
  before the tag, then the tag must still name the commit. **When SABnzbd
  changes its release key**, its Renovate PR fails at the fetch stage,
  naming the commit and the key it was signed with: check the new key
  from more than one source (GitHub's `https://github.com/Safihre.gpg`,
  SABnzbd's own announcements), export the new primary key alone, minimal,
  into `keys/sabnzbd-release.asc`, and change the fingerprint in the
  Containerfile, in one commit a person makes and signs
- **The Python packages come from the locks alone.** `python/files.txt`
  names every file both locks take, by sha256 and URL; the deps stage
  fetches each with curl and checks it with sha256sum, since `pip download`
  would run each source release's build backend with the network on.
  Everything after runs with `--network=none`. The packages with C or Rust
  code build from source with `--no-build-isolation
  --check-build-dependencies`, from the build venv's backends: cffi first,
  since cryptography's build calls it; cryptography against DHI's OpenSSL
  (`OPENSSL_NO_VENDOR=1`), and its Rust crates fetched by cargo with the
  network, checked by its `Cargo.lock`, then built offline. `pip check`
  must pass, and the venv holds no pip
- **unrar and 7zz link to the runtime's glibc and libstdc++**, and each
  must pass `check-elf`: PIE, `BIND_NOW`, `GNU_RELRO`, and no library the
  runtime image does not ship. unrar must print "Alexander Roshal" and be
  5.50 or newer; 7zz must list no RAR. Both build with
  `-D_FORTIFY_SOURCE=3 -fstack-protector-strong -fPIE` and
  `-pie -Wl,-z,relro,-z,now`; 7-Zip's makefiles take them through
  `MY_ARCH` and `LDFLAGS`, since they read no CFLAGS
- **The SBOM is written from the same ARGs** that pin unrar and 7-Zip, and
  unrar's version from `version.hpp`, so it cannot go stale. Its label is
  `io.github.divbast.scan.sboms`, which ci's scan reads; neither scanner
  sees a binary built here otherwise
- **Every ARG pair stays adjacent and in order** (`SABNZBD_VERSION` then
  `SABNZBD_COMMIT`, `UNRAR_VERSION` then `UNRAR_SHA256`, `SEVENZIP_VERSION`
  then `SEVENZIP_COMMIT`): Renovate's managers read them that way
- **hadolint forgets a stage's `SHELL` when an `ARG` or `COPY` comes
  between it and the `RUN`**: each stage puts `SHELL` right before its
  first `RUN`

## Python dependencies

- **`go run ./cmd/divbast-pins`** writes `python/requirements.lock`,
  `python/build.lock`, `python/files.txt` and the Containerfile's
  `UNRAR_SHA256` from what the repository pins: SABnzbd's
  `requirements.txt` at `SABNZBD_COMMIT`, read for Linux and the
  Containerfile's DHI Python; upstream's `==` pins exactly; and
  `python/pins.txt`. `just lint` runs it with `-check`, which fails with a
  diff when a file is stale. It needs the network
- **`python/pins.txt`** holds one `package==version` a line, each under a
  comment saying which kind it is: `# upstream leaves it unpinned: <its
  requirement>` (cryptography), `# needed by <package> …` (tzdata, which
  python-dateutil 3.0.0 requires and upstream's list leaves out), or
  `# builds <packages> from source …`, for the build venv alone. A pin for
  a package upstream pins with `==` fails: upstream's pin stands
- **A pure-Python package installs from its pure wheel; any other from
  its source release.** The lock's `--no-binary` line names the source
  builds. `build.lock` takes a pure wheel, or every Linux glibc wheel of
  both architectures (maturin, cmake)
- **setuptools is held at 82.0.1** by a Renovate rule: httptools 0.9.0,
  which SABnzbd pins, builds with nothing newer. Lift the rule when
  SABnzbd's httptools does

## Dependency updates

Renovate, through divbast/ci's `renovate.yml`, opens a PR for each new
input and merges it once every check passes, while `SABNZBD_AUTOMERGE` is
`enabled`:

- SABnzbd's GitHub releases, pre-releases included, every tag on develop,
  read by a versioning that sorts `5.2.0RC2` below `5.2.0`;
- unrar's tarball, from RARLAB's `rar_add.htm`, which links the newest,
  betas included. The sha256 is that of what RARLAB served when the PR was
  made: nothing shows the first download is RARLAB's own, and Debian's
  `unrar-nonfree` source package carries the same tarball, a few days
  later, for a person to compare;
- 7-Zip's GitHub releases, with the commit;
- each line of `python/pins.txt`, from PyPI;
- the DHI images, the Python runtime and `-dev` grouped by the app
  preset, and the toolchain stage's DHI packages, grouped here.

A PR that moves SABnzbd, unrar or a pin runs `go run ./cmd/divbast-pins`
as a post-upgrade task, which ci's `RENOVATE_ALLOWED_COMMANDS` allows, so
the locks and the checksum move with it. A new SABnzbd whose requirements
need a pin that `pins.txt` lacks fails that task, naming the line to add.

## Where the shared parts live

- **divbast/ci**, checked out by `just setup` into `.ci/` at the one commit
  every workflow pins: the reusable workflows, the recipes the justfile
  imports, the Go tools in `.ci/bin/` (`divbast-verify-images`,
  `divbast-scan`, `divbast-testcount` and the rest), the acceptance
  `harness` and the `contract` suite, and `go/golangci.yml`
- **divbast/renovate-config**, the presets `renovate.json` extends, beside
  this repository's own managers
- **divbast/infra**, the repository's settings, rulesets, `renovate`
  environment, `SABNZBD_AUTOMERGE`, and the DHI login

## Key commands

- `just setup`: divbast/ci into `.ci/`, then the toolchain from `mise.toml`
- `just build`: the image, `localhost/sabnzbd:dev`. It does not verify the
  images it builds from; `just ci-tools && .ci/bin/divbast-verify-images`
  does, with `DHI_USERNAME` and `DHI_TOKEN` set
- `just acceptance`, `just test`: the suite, under `ENGINE`, through
  `divbast-testcount`
- `just lint`: every lint gate, including `pins-check`. Needs Docker,
  logged in to dhi.io, for `docker build --check`
- `just archive`, `just scan`: the image as an OCI archive, then
  `divbast-scan` over it and the published `:latest`
- `go run ./cmd/divbast-pins`: the locks and unrar's checksum, from the pins
- `just ci`: every gate, locally. Run this before committing

## Code quality

- All linter gates are enforced as errors. Any suppression needs a
  justification beside it
- **The acceptance suite is Go, on divbast/ci's harness**: every container
  runs under `harness.Contract("128m")`. It sets up volumes and reads files
  with the image's own Python, as root only for setup, and needs no app
  probe of its own. Unpacking is tested with the image's unrar and 7zz, on
  a RAR the test writes and a 7z 7zz makes: SABnzbd unpacks a job only
  after downloading it, which needs a news server
- **Go is modern and idiomatic, on the standard library alone**, under
  ci's `go/golangci.yml`
- A test that has never failed proves nothing: when adding one, break what
  it guards and watch it fail
- REUSE-compliant SPDX headers on every file. This repository is
  **GPL-3.0-only**; the image's licences label lists what it ships:
  SABnzbd's, every Python package's, unrar's and 7-Zip's
