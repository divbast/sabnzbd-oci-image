// SPDX-License-Identifier: GPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package pins

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Args is what the tool reads from the Containerfile: the SABnzbd commit
// whose requirements it locks, the Python the image runs, as major.minor,
// and unrar's tarball and its checksum.
type Args struct {
	SABnzbdCommit, PythonVersion, UnrarVersion, UnrarSHA256 string
}

// pythonTag is a DHI Python image's version, in its tag.
var pythonTag = regexp.MustCompile(`dhi\.io/python:(\d+)\.(\d+)\.(\d+)-`)

var (
	commitHex  = regexp.MustCompile(`^[0-9a-f]{40}$`)
	unrarValue = regexp.MustCompile(`^\d+(\.\d+)*$`)
)

// arg returns the value of the one ARG name=value line of containerfile.
func arg(containerfile []byte, name string) (string, error) {
	m := regexp.MustCompile(`(?m)^ARG `+regexp.QuoteMeta(name)+`=(.*)$`).FindAllSubmatch(containerfile, -1)
	if len(m) != 1 {
		return "", fmt.Errorf("the Containerfile must hold one ARG %s=<value> line, not %d", name, len(m))
	}
	return strings.TrimSpace(string(m[0][1])), nil
}

// ReadArgs reads Args from containerfile. Every dhi.io/python image it
// names must be one Python, since the venv the -dev image builds runs on
// the runtime image's.
func ReadArgs(containerfile []byte) (Args, error) {
	var (
		a    Args
		errs []error
	)
	for _, v := range []struct {
		name string
		to   *string
		ok   *regexp.Regexp
	}{
		{"SABNZBD_COMMIT", &a.SABnzbdCommit, commitHex},
		{"UNRAR_VERSION", &a.UnrarVersion, unrarValue},
		{"UNRAR_SHA256", &a.UnrarSHA256, sha256Hex},
	} {
		value, err := arg(containerfile, v.name)
		switch {
		case err != nil:
			errs = append(errs, err)
		case !v.ok.MatchString(value):
			errs = append(errs, fmt.Errorf("the Containerfile's %s is %q, which does not match %s", v.name, value, v.ok))
		default:
			*v.to = value
		}
	}
	var versions []string
	for _, m := range pythonTag.FindAllSubmatch(containerfile, -1) {
		if v := fmt.Sprintf("%s.%s.%s", m[1], m[2], m[3]); !slices.Contains(versions, v) {
			versions = append(versions, v)
		}
	}
	switch len(versions) {
	case 0:
		errs = append(errs, errors.New("the Containerfile names no dhi.io/python:<x.y.z>- image, so nothing says which Python the venv is for"))
	case 1:
		a.PythonVersion = versions[0][:strings.LastIndex(versions[0], ".")]
	default:
		errs = append(errs, fmt.Errorf("the Containerfile names Pythons %s: the -dev image and the runtime must be one", strings.Join(versions, " and ")))
	}
	if err := errors.Join(errs...); err != nil {
		return Args{}, err
	}
	return a, nil
}

// SetArg returns containerfile with its one ARG name= line set to value.
func SetArg(containerfile []byte, name, value string) ([]byte, error) {
	if _, err := arg(containerfile, name); err != nil {
		return nil, err
	}
	re := regexp.MustCompile(`(?m)^ARG ` + regexp.QuoteMeta(name) + `=.*$`)
	return re.ReplaceAllLiteral(bytes.Clone(containerfile), []byte("ARG "+name+"="+value)), nil
}
