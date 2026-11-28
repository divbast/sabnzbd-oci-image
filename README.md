<!-- SPDX-License-Identifier: GPL-3.0-only -->
<!-- SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee> -->

# sabnzbd-oci-image

[SABnzbd](https://github.com/sabnzbd/sabnzbd), the Usenet downloader, in an
image that runs at zero Linux capabilities, with no new privileges, on a
read-only root filesystem as a non-root user, with no shell in it. It is
built from SABnzbd's source, and goes through divbast/ci's shared pipeline:
lint, a build for amd64 and arm64, the confinement contract and its own
acceptance suite under Docker and rootless Podman, an image scan, and a
daily publish to `ghcr.io/divbast/sabnzbd`. [Publishing](#publishing) says
when it publishes and under which tags.

The image follows every release SABnzbd tags on its develop branch:
pre-releases, such as `5.2.0RC2` or `5.3.0Beta1`, as well as stable ones.

## Base

The image is a SABnzbd release, built from source: the version and commit
the Containerfile's `SABNZBD_VERSION` and `SABNZBD_COMMIT` name. It runs on
Docker Hardened Images' [`python`](https://hub.docker.com/hardened-images/catalog)
for Debian 13, with no shell and no package manager. That image ships pip,
setuptools and wheel beside Python, and this one keeps them: nothing here
runs them, and SABnzbd's own packages are in a venv of their own, with no
pip in it. Every stage of the build is a Docker Hardened Image too, and CI
refuses to build until Docker's signature on each is verified. Every
package the build stages install comes from Docker Hardened Images' own
Debian repository alone.

- `fetch` checks out SABnzbd's commit, checks that it is signed by
  SABnzbd's release key, the one in `keys/`, and no other, and that the
  release's tag still names it. It compiles SABnzbd's translations as
  SABnzbd's own packaging does.
- `deps` builds SABnzbd's Python packages. Each file is fetched first, by
  the sha256 that `python/files.txt` names, and checked; then, with no
  network, the pure-Python packages are installed from their wheels, and
  the ones with C or Rust code (sabctools, CT3, cffi, httptools,
  cryptography, ujson, orjson and PyYAML) are built from source, against
  the image's own OpenSSL and libraries. No package's install script runs
  outside its build. `pip check` then holds every package to what it
  requires.
- `sevenzip` builds 7-Zip's `7zz` from its repository, without RAR
  support. It links to the base's C and C++ libraries, so Docker's updates
  of the base reach it, and is hardened and checked: position-independent,
  bound at load, with a read-only relocation table.
- unrar, which SABnzbd needs to unpack RAR archives, is not built here:
  the final image copies it, with its licence, from the
  `ghcr.io/divbast/unrar` carrier of
  [divbast/native-tools](https://github.com/divbast/native-tools), by
  digest. ci verifies that digest's attestation before the image builds.
- `sbom` writes an SBOM of unrar and 7zz, which the scan reads: unrar's
  component is the carrier's own, with its CPE, and only its path here is
  added. See [The scan](#the-scan).
- `probe` builds `divbast-sabnzbd`, the image's own command, a static Go
  binary on the standard library alone.

SABnzbd is never patched.

## Container Features

| Property | Value |
|---|---|
| Capabilities | none (`--cap-drop ALL`) |
| Privileges | no new ones (`--security-opt no-new-privileges`), under the engine's seccomp filter |
| Root filesystem | read-only |
| User | uid/gid 65532 (`nonroot`) |
| Port | 8080 (the web UI and the API), on IPv4 and IPv6, and nothing else listens |
| tmpfs required | `/tmp`, which nothing can run from (`--tmpfs /tmp:size=128m,noexec,nosuid,nodev`); see [`/tmp`](#tmp) |
| Volume | `/config`, which you mount, owned by 65532; and your [downloads](#downloads-and-hardlinks) |
| Processes | one: SABnzbd, as PID 1, which runs `unrar` and `7zz` as it unpacks |
| Shell in image | none |
| Package manager | none |
| Outbound network | your news servers, and whatever you configure; SABnzbd's [release check](#release-check) |
| Healthy | the shipped probe: `GET /api?mode=version` answers this image's version, and SABnzbd listens on 8080 on every address |

## Running

Pull `ghcr.io/divbast/sabnzbd:latest`, which follows SABnzbd's newest
release; a version tag, such as `:5.2.0RC2`, pins one. The package page on
ghcr.io lists the tags. To build it yourself: `docker login dhi.io`, since
every image the build starts from is Docker Hardened, then `just setup`,
then `ENGINE=podman just build`, which tags it `localhost/sabnzbd:dev`.

### Podman

```bash
podman run -d --name sabnzbd \
  --cap-drop ALL --read-only --read-only-tmpfs=false \
  --security-opt no-new-privileges \
  --user 65532:65532 \
  --tmpfs /tmp:size=128m,noexec,nosuid,nodev \
  -v sabnzbd-config:/config \
  -v /srv/data:/data \
  -p 127.0.0.1:8080:8080 \
  --health-cmd '["/usr/local/bin/divbast-sabnzbd","health"]' \
  ghcr.io/divbast/sabnzbd:latest
```

`--read-only-tmpfs=false` and `--health-cmd` are Podman's alone. Without
the first, Podman mounts writable tmpfs of its own at `/run`, `/var/tmp`
and elsewhere beside the read-only root; without the second, it never
runs the image's health check, as [Health](#health) says. `/srv/data`,
which SABnzbd shares with Sonarr, Radarr or Lidarr, must be writable by
65532, as [Downloads and hardlinks](#downloads-and-hardlinks) says.

As a Quadlet, in `~/.config/containers/systemd/sabnzbd.container`:

```ini
[Container]
Image=ghcr.io/divbast/sabnzbd:latest
User=65532
Group=65532
DropCapability=ALL
NoNewPrivileges=true
ReadOnly=true
ReadOnlyTmpfs=false
Tmpfs=/tmp:size=128m,noexec,nosuid,nodev
Volume=sabnzbd-config:/config
Volume=/srv/data:/data
PublishPort=127.0.0.1:8080:8080
HealthCmd=["/usr/local/bin/divbast-sabnzbd","health"]

[Install]
WantedBy=default.target
```

### Docker and Compose

Docker takes the Podman command without `--read-only-tmpfs=false` and
`--health-cmd`, and runs the image's `HEALTHCHECK` itself. With Compose:

```yaml
services:
  sabnzbd:
    image: ghcr.io/divbast/sabnzbd:latest
    user: "65532:65532"
    read_only: true
    cap_drop: [ALL]
    security_opt: ["no-new-privileges:true"]
    environment:
      SABNZBD_HOST_WHITELIST: sabnzbd
    tmpfs:
      - /tmp:size=128m,noexec,nosuid,nodev
    volumes:
      - sabnzbd-config:/config
      - /srv/data:/data
    ports:
      - "127.0.0.1:8080:8080"
volumes:
  sabnzbd-config:
```

`SABNZBD_HOST_WHITELIST: sabnzbd` lets Sonarr, Radarr or Lidarr on the
same Compose network reach SABnzbd at `http://sabnzbd:8080`: until a
login is set, SABnzbd refuses a name it does not know, as
[Hostnames and access](#hostnames-and-access) says. A bind mount such as
`/srv/data` must be writable by 65532: `chown -R 65532:65532 /srv/data`
as root.

### Kubernetes

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: sabnzbd
spec:
  replicas: 1
  strategy:
    type: Recreate
  selector:
    matchLabels: {app: sabnzbd}
  template:
    metadata:
      labels: {app: sabnzbd}
    spec:
      securityContext:
        runAsUser: 65532
        runAsGroup: 65532
        runAsNonRoot: true
        fsGroup: 65532
        fsGroupChangePolicy: OnRootMismatch
        seccompProfile: {type: RuntimeDefault}
      containers:
        - name: sabnzbd
          image: ghcr.io/divbast/sabnzbd:latest
          env:
            - name: SABNZBD_HOST_WHITELIST
              value: sabnzbd,sabnzbd.example.com
          ports:
            - {name: http, containerPort: 8080}
          securityContext:
            readOnlyRootFilesystem: true
            allowPrivilegeEscalation: false
            capabilities: {drop: [ALL]}
          volumeMounts:
            - {name: config, mountPath: /config}
            - {name: data, mountPath: /data}
            - {name: tmp, mountPath: /tmp}
          startupProbe:
            httpGet:
              path: /api?mode=version
              port: http
            failureThreshold: 30
            periodSeconds: 2
          livenessProbe:
            httpGet:
              path: /api?mode=version
              port: http
            periodSeconds: 30
            timeoutSeconds: 4
      volumes:
        - name: config
          persistentVolumeClaim: {claimName: sabnzbd-config}
        - name: data
          persistentVolumeClaim: {claimName: data}
        - name: tmp
          emptyDir: {medium: Memory, sizeLimit: 128Mi}
---
apiVersion: v1
kind: Service
metadata:
  name: sabnzbd
spec:
  selector: {app: sabnzbd}
  ports:
    - {name: http, port: 8080, targetPort: http}
```

- `fsGroup` gives the volumes to group 65532, which the image runs as, so
  SABnzbd can write them.
- One replica, and `Recreate`: two SABnzbd processes must never share
  `/config`.
- An `emptyDir` cannot be mounted `noexec`, as the `--tmpfs` above is.
- `SABNZBD_HOST_WHITELIST` names the Service's short name and the Ingress
  host; names under `*.local`, such as `sabnzbd.media.svc.cluster.local`,
  are always let in, as [Hostnames and access](#hostnames-and-access) says.
- A cluster whose pod network is in `100.64.0.0/10` must list that range,
  with every other range its clients use, in SABnzbd's local ranges, since
  SABnzbd counts it as outside: see
  [Hostnames and access](#hostnames-and-access).

### First start

SABnzbd serves its web UI and API on 8080. On an empty `/config`, the
image writes SABnzbd's first settings, then starts SABnzbd, and the first
visit to the web UI opens SABnzbd's setup wizard. **SABnzbd has no login
until you set one** in Config, General, Security. It answers only clients
on your local network (loopback and private addresses), and refuses the
rest until Config, General, "External internet access" lets them in;
until a login is set, it also checks the name a browser asked for. The
commands above publish the port on the host's loopback alone:
open `http://127.0.0.1:8080` there first, set a username and a password,
and only then publish the port beyond the host, or put SABnzbd behind your
reverse proxy.

A stop (SIGTERM) ends an idle SABnzbd within a second, with exit 0. SABnzbd's own
Restart, in its web UI, happens inside the container, which keeps
running; see [Restart](#restart).

### `/config`

The image carries an empty `/config` owned by 65532, and declares no
`VOLUME`. A new named volume takes its owner from it on first mount, so
`-v sabnzbd-config:/config` just works. SABnzbd keeps everything it writes
there: `sabnzbd.ini`, its queue and history, and, unless you choose other
folders, its downloads under `/config/Downloads`.

- A `/config` that 65532 cannot write, such as a bind mount root made, or
  a directory another image left owned by its own user, stops the start
  at once, exit 1, with a line naming `/config` and uid 65532. Give the
  directory, and everything in it, to 65532 instead of running
  the image as root: `chown -R 65532:65532 <dir>` as root, or under
  rootless Podman `podman unshare chown -R 65532:65532 <dir>`. For a
  Podman bind mount, `:U` does the same at start: `-v ./config:/config:U`.
- On an SELinux host, such as Fedora or RHEL, a bind mount also needs
  Podman to relabel it, or the container cannot use it: add `:Z`, as in
  `-v ./config:/config:U,Z`. `/config` is SABnzbd's alone, so the private
  label `Z` fits it.
- With no mount at all, the read-only root refuses the first write, and
  the start stops at once, exit 1, saying so. Nothing would have been kept.

### Coming from another image

Stop the old container, and keep a copy of its `/config`. Give the
directory to 65532, as above, and mount it at `/config`. The image leaves
an existing `sabnzbd.ini` as it is: `SABNZBD_HOST_WHITELIST` and the first
settings below apply to a new `/config` only. SABnzbd still listens on
8080 whatever port the old settings name.

- Download folders keep the paths the old settings give, such as
  `/downloads` or `/incomplete-downloads`: mount your folders there, or
  change them in Config, Folders. A folder SABnzbd cannot create is a
  warning in its web UI, not a stop.
- Settings another image wrote may leave SSDP on: turn
  `enable_broadcast` off in Config, Special, as this image's first
  settings do.

### `/tmp`

SABnzbd stages each NZB you upload in `/tmp`, twice over while it reads
it, and a copy of its history database while it writes a backup. 128 MiB
takes uploads of NZB files up to 50 MB; a larger one is refused with
SABnzbd's own error, and the container stays healthy. An upload that
fails can leave its temporary copy in `/tmp` until the container
restarts, and each copy left there lowers what fits. Raise `size=` for
larger NZB files, or a long history.

## Configuration

SABnzbd reads its settings from `/config/sabnzbd.ini`, which its web UI
writes; the image reads one environment variable, on first start alone.

### First settings

On a `/config` with no `sabnzbd.ini`, the image writes one, with three
settings, before SABnzbd first starts, and SABnzbd fills in the rest:

- `enable_broadcast = 0`: SSDP off, so SABnzbd does not announce itself on
  your network every 15 seconds;
- `fixed_ports = 1`: a port 8080 already taken stops the start, exit 2,
  rather than SABnzbd moving to another port and saving it;
- `host_whitelist`, from `SABNZBD_HOST_WHITELIST`, when that is set.

From then on, SABnzbd's web UI owns every setting, and the image never
writes the file again.

### Hostnames and access

SABnzbd answers a request only from a local address, unless "External
internet access" lets others in, and, until a login is set, only for a
name it knows: `localhost`, an IP address, a name under `*.local`, or one
in its host whitelist. That stops another web page, opened in a browser
on your network, from reaching SABnzbd through a name it controls.

- `SABNZBD_HOST_WHITELIST` sets the whitelist on first start: names
  separated by commas, such as `sabnzbd,sabnzbd.example.com`, each a host
  name or an IP address, with no port or scheme. A value that is not is
  refused, and the start stops with a line naming the entry. Later, change
  it in Config, Special, `host_whitelist`.
- Without it, SABnzbd puts the container's hostname there, which changes
  when the container is made again.
- SABnzbd counts loopback and private addresses as local. A network in
  `100.64.0.0/10`, which some Kubernetes clusters and VPNs use, is not.
  Config, Special, `local_ranges` changes that, but it replaces SABnzbd's
  own list of private ranges rather than adding to it: list
  `100.64.0.0/10` together with every range your clients use, such as
  `192.168.0.0/16`, or SABnzbd refuses them. Loopback is always let in.
  The image has no shell, so a list that leaves your browser out can be
  undone only by editing `local_ranges` in `sabnzbd.ini` on the volume.

### Behind a reverse proxy

Give the proxy's host name in the whitelist, and point the proxy at port
8080. SABnzbd serves at `/` and, when Config, Special, `url_base` is set,
under that path as well. A proxy on a local address is trusted to say who
its client is, in `X-Forwarded-For`, so SABnzbd refuses clients from the
internet that reach it through the proxy until "External internet access"
lets them in; set a login first.

### Release check

SABnzbd checks once a day, and ten minutes after it starts, for a newer
release, by fetching `https://sabnzbd.org/latest.txt`, and shows a notice
in its web UI when there is one. The request names SABnzbd's version, in
its User-Agent, and nothing of your settings or downloads; sabnzbd.org
sees your address. Turn it off in Config, General, "Check for New
Release", which writes `check_new_rel = 0`. This
image does not update SABnzbd from inside: a new release arrives as a new
image.

### Downloads and hardlinks

SABnzbd downloads to `/config/Downloads/incomplete` and
`/config/Downloads/complete` until you set other folders in Config,
Folders. Sonarr, Radarr and Lidarr import what SABnzbd finishes, from the
path SABnzbd reports, and hardlinking, which imports instantly and without
a second copy, needs the download and the library on one filesystem. So
give SABnzbd and them one shared `/data` mount, writable by 65532, with
SABnzbd's folders at `/data/downloads/incomplete` and
`/data/downloads/complete` and the libraries beside them, not a mount
each: then the path SABnzbd reports is one they can open, with no remote
path mapping. divbast's Sonarr, Radarr and Lidarr run as 65532 too, so
they can link the files SABnzbd writes. On an SELinux host, a `/data`
bind mount several containers share takes Podman's `:z`, the shared
label, not `:Z`, which would keep it to one container. Leave `:U` off it:
Podman would change the owner of everything in it at every start.

### User scripts

The image has no shell, so a script SABnzbd runs after a download cannot
be a shell script: one that starts `#!/bin/sh` or `#!/usr/bin/env` does not
run. A Python script, named `.py` and executable (`chmod +x`), with no
first line naming an interpreter, runs with the image's Python. Put scripts in a folder under
`/config` and set it in Config, Folders.

### Restart

SABnzbd's own Restart starts SABnzbd again inside the container, which
keeps running. After it, SABnzbd also writes its log to files under
`/config/logs`, beside the engine's log, since it starts again with fewer
of the image's options.

### Restoring a backup

Config, General, Restore takes a backup's settings whole, where SABnzbd
listens included, and SABnzbd then restarts itself, inside the container,
listening there. A backup from SABnzbd installed on a host says
`127.0.0.1`, which the container's port cannot reach, so SABnzbd stops
answering from outside, and the [health check](#health) fails, naming
it. **Restart the container after a restore:** the image starts SABnzbd
on every address, on port 8080, again, and SABnzbd saves that. A restore
also brings back the backup's SSDP setting: if it was on, turn
`enable_broadcast` off again in Config, Special.

### Not in the image

SABnzbd looks for `nice` and `ionice` to lower its tools' priority, and
for `df` to tell whether a folder is on a FAT filesystem; none is in the
image. SABnzbd logs that and carries on: the tools run at normal
priority, and SABnzbd takes every folder to be on a filesystem other than
FAT. There is no `par2`
program: SABnzbd's own sabctools module does the repairs.

## Health

`divbast-sabnzbd health` asks SABnzbd, from inside the container, for
`/api?mode=version`, which needs no API key, and checks that the answer
is this image's version. It also checks, in the container's
`/proc/net/tcp6` and `/proc/net/tcp`, that SABnzbd listens on 8080 on
every address, so the container's port reaches it: SABnzbd listens where
its settings say once it restarts itself, which after a
[restored backup](#restoring-a-backup) can be 127.0.0.1 alone. It prints
nothing and exits 0 when both hold, and one `version: <reason>` or
`listen: <reason>` line and exit 1 when one does not. The image's
`HEALTHCHECK` runs it every 30 seconds; Docker reads it. Podman never sees
the `HEALTHCHECK` of an image published as an OCI index, so pass the same
command as `--health-cmd '["/usr/local/bin/divbast-sabnzbd","health"]'`,
or `HealthCmd=` in a Quadlet, as above. In Kubernetes, the probes above ask
SABnzbd for the same path from outside the pod, so they fail too when
SABnzbd listens on 127.0.0.1 alone, and Kubernetes restarts it.

## The pipeline

Every pull request and push to main runs divbast/ci's `app-check.yml`: lint
(the Go code, the Containerfile, the workflows, REUSE, and the Python locks
against their pins), a build for amd64 and arm64 once every image the build
starts from is verified, the acceptance suite under Docker and rootless
Podman, and the scan. A daily slot publishes main when it has moved since
the last image.

### Publishing

A merge never publishes. The daily slot publishes `main` when `main` moved
since the commit the published `:latest` was built from, and the button, a
manual run of `build` from `main`, publishes at once.

Its publish job calls divbast/ci's `app-publish.yml` with `image:
ghcr.io/divbast/sabnzbd`. A publish pushes an index of the amd64 and arm64
images, attests it, verifies the attestation, and only then tags it twice:
with SABnzbd's version, the Containerfile's `SABNZBD_VERSION`, and with
`latest`. It ends by checking, as an anonymous client, that both tags are
the attested index.

### The scan

The scan fails a change only on a HIGH or CRITICAL finding the published
image does not have already. unrar and 7zz are not packages, so no image
scan can tell what they are; the image names both in an SBOM,
`/usr/local/share/divbast/sbom.cdx.json`, which its
`io.github.divbast.scan.sboms` label declares, and the scan judges them by
it, as it does every package.

## Dependency updates

Renovate opens a pull request for each new input, and merges it once
every check passes:

- each new SABnzbd tag on develop, with its commit; the fetch stage checks
  the commit's signature. A release signed by any other key stops at its
  pull request until a person checks the new key and replaces
  `keys/sabnzbd-release.asc`;
- each new digest of the unrar carrier, `ghcr.io/divbast/unrar:latest`,
  which divbast/native-tools publishes;
- each new 7-Zip release, with its commit;
- each line of `python/pins.txt`: what SABnzbd's requirements leave
  unpinned, cryptography today, what a pinned package needs that they
  leave out, and the packages the source builds need;
- the Docker Hardened Images, the Python runtime and its `-dev` image
  together, and the toolchain stage's packages.

A pull request that moves SABnzbd or a pin runs
`go run ./cmd/divbast-pins` too, which writes the locks again from the
new pins, so they move together. setuptools stays
at 82.0.1, the newest httptools, which SABnzbd pins, builds with.

So that Renovate does not propose a pin the build must refuse, the
command also writes `python/renovate-bounds.json`, a Renovate preset that
`renovate.json` extends: for each pin that SABnzbd's requirements, or a
package of the locks, bound, every version range asked of it, as
`pip check` reads the packages' requirements. Renovate reads it from
`main`, so a bound lifts once the pull request that moves its source
merges.

The repository variable `SABNZBD_AUTOMERGE` stops Renovate merging when it
is anything but `enabled`:

```bash
gh variable set SABNZBD_AUTOMERGE --body disabled
```

## Development

- `just setup` checks out divbast/ci at the commit the workflows pin and
  installs the toolchain.
- `just build`, `just acceptance` and `just test` build the image and run
  the acceptance suite, under the engine `ENGINE` names.
- `just lint` runs every lint gate, and `just pins-check`, which needs the
  network.
- `go run ./cmd/divbast-pins` writes `python/requirements.lock`,
  `python/build.lock`, `python/files.txt` and
  `python/renovate-bounds.json` from the pins, after a change
  to `python/pins.txt` or the Containerfile's pins.

## AI/LLM Disclosure

This project was developed with significant LLM involvement. Each git commit contains an `Assisted-by:` tag detailing the particular model/tool used.
