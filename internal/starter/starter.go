// SPDX-License-Identifier: GPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

// Package starter writes SABnzbd's first settings, once: the file SABnzbd
// starts from when /config holds none. After that, SABnzbd's own web UI
// owns every setting, and the starter never reads or writes the file
// again.
package starter

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// File is SABnzbd's settings file, in its config directory.
const File = "sabnzbd.ini"

// Variable is the environment variable that names the host names, beside
// localhost, IP addresses and *.local names, SABnzbd answers to on first
// start.
const Variable = "SABNZBD_HOST_WHITELIST"

// label is one label of a DNS name.
var label = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// ParseWhitelist reads s, the value of SABNZBD_HOST_WHITELIST, as SABnzbd's
// host_whitelist: names separated by commas, each a DNS name or an IP
// address, and an IPv6 address bare or in brackets. Each is trimmed and
// lower-cased, empty entries are dropped, and a name given twice is kept
// once. An entry with a port, a scheme, a path, a wildcard or anything
// else that is not a host name is refused, naming it.
func ParseWhitelist(s string) ([]string, error) {
	var out []string
	for e := range strings.SplitSeq(s, ",") {
		e = strings.ToLower(strings.TrimSpace(e))
		if e == "" {
			continue
		}
		host, err := hostName(e)
		if err != nil {
			return nil, fmt.Errorf("%s: %q %w", Variable, e, err)
		}
		if !slices.Contains(out, host) {
			out = append(out, host)
		}
	}
	return out, nil
}

// hostName reads e as a host name or an IP address, as SABnzbd compares a
// request's Host with it.
func hostName(e string) (string, error) {
	bare := strings.TrimSuffix(strings.TrimPrefix(e, "["), "]")
	if ip := net.ParseIP(bare); ip != nil && (bare == e || strings.Contains(bare, ":")) {
		return ip.String(), nil
	}
	switch {
	case strings.Contains(e, "://"):
		return "", errors.New("has a scheme: give the host name alone")
	case strings.ContainsAny(e, "/?#"):
		return "", errors.New("has a path: give the host name alone")
	case strings.Contains(e, "*"):
		return "", errors.New("is a wildcard, which SABnzbd does not read: name each host")
	case strings.Contains(e, ":"):
		return "", errors.New("has a port: give the host name alone, since SABnzbd compares names without ports")
	}
	if len(e) > 253 {
		return "", errors.New("is longer than a host name can be")
	}
	for l := range strings.SplitSeq(e, ".") {
		if !label.MatchString(l) {
			return "", errors.New("is not a host name: each part holds letters, digits and hyphens, and starts and ends with a letter or digit")
		}
	}
	return e, nil
}

// Starter is the file Ensure writes: SSDP off, the port fixed, and the
// whitelist, when there is one.
func Starter(whitelist []string) string {
	s := "[misc]\nenable_broadcast = 0\nfixed_ports = 1\n"
	if len(whitelist) > 0 {
		s += "host_whitelist = " + strings.Join(whitelist, ", ") + "\n"
	}
	return s
}

// Ensure writes dir's sabnzbd.ini, the Starter for the whitelist env's
// SABNZBD_HOST_WHITELIST gives, when dir holds no sabnzbd.ini, and reports
// whether it wrote one. A file there already is left unread and
// unchanged, whatever it holds and whoever may read it. The file is
// written to a temporary file in dir, mode 0600, and linked into place,
// so it never overwrites one that appeared meanwhile; it is then left as
// it is. A bad whitelist, or a dir that cannot be written, writes nothing.
func Ensure(dir string, env func(string) string) (wrote bool, err error) {
	path := filepath.Join(dir, File)
	if _, err = os.Lstat(path); err == nil {
		return false, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return false, fmt.Errorf("cannot tell whether %s exists: %w", path, err)
	}
	whitelist, err := ParseWhitelist(env(Variable))
	if err != nil {
		return false, fmt.Errorf("%w; nothing was written to %s", err, dir)
	}
	tmp, err := os.CreateTemp(dir, "."+File+".*")
	if err != nil {
		return false, fmt.Errorf("cannot write the first settings in %s, which must be a directory uid %d can write: %w", dir, os.Getuid(), err)
	}
	defer func() {
		if rerr := os.Remove(tmp.Name()); rerr != nil && !errors.Is(rerr, fs.ErrNotExist) {
			err = errors.Join(err, rerr)
		}
	}()
	_, err = tmp.WriteString(Starter(whitelist))
	if err == nil {
		err = tmp.Sync()
	}
	err = errors.Join(err, tmp.Close())
	if err != nil {
		return false, fmt.Errorf("write %s: %w", tmp.Name(), err)
	}
	if err = os.Link(tmp.Name(), path); errors.Is(err, fs.ErrExist) {
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	return true, syncDir(dir)
}

// syncDir makes dir's new entry durable.
func syncDir(dir string) error {
	d, err := os.Open(dir) //nolint:gosec // G304: the config directory the image names
	if err != nil {
		return err
	}
	return errors.Join(d.Sync(), d.Close())
}
