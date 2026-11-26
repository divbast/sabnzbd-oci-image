// SPDX-License-Identifier: GPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package acceptance

import (
	"net/http"
	"regexp"
	"testing"

	"github.com/divbast/ci/harness"
)

// whitelistVar is the one variable the image reads, on first start alone.
const whitelistVar = "SABNZBD_HOST_WHITELIST"

// iniValue reads key's value in ini, "" when it has none.
func iniValue(ini, key string) string {
	m := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(key) + ` = (.*)$`).FindStringSubmatch(ini)
	if m == nil {
		return ""
	}
	return m[1]
}

// TestStarter is the first start's settings: SSDP off, the port fixed, the
// whitelist from SABNZBD_HOST_WHITELIST, SABnzbd's own release check left
// on; then a start on the same /config, whose settings the command leaves
// alone, whatever the variable says; then a malformed variable, which
// stops the start and writes nothing.
func TestStarter(t *testing.T) {
	t.Parallel()
	e, image := underTest(t)
	checks := harness.NewChecks(t)
	s, vol := serveFresh(t, e, image, map[string]string{whitelistVar: " Sab, sab.example.com,"})
	ini, err := readINI(t, s)
	if err != nil {
		t.Fatal(err)
	}
	harness.Equal(checks, "SSDP is off", "0", iniValue(ini, "enable_broadcast"))
	harness.Equal(checks, "the port is fixed", "1", iniValue(ini, "fixed_ports"))
	harness.Equal(checks, "host_whitelist is the variable's, normalised", "sab, sab.example.com", iniValue(ini, "host_whitelist"))
	harness.Equal(checks, "SABnzbd's release check stays on", "1", iniValue(ini, "check_new_rel"))
	key := iniValue(ini, "api_key")
	checks.True("SABnzbd made its own API key", len(key) == 32, key)
	stop(t, e, s.C)

	again := serveSAB(t, e, image, map[string]string{whitelistVar: "other.example.com"}, vol)
	ini, err = readINI(t, again)
	if err != nil {
		t.Fatal(err)
	}
	harness.Equal(checks, "a later start ignores the variable", "sab, sab.example.com", iniValue(ini, "host_whitelist"))
	harness.Equal(checks, "a later start keeps SABnzbd's API key", key, iniValue(ini, "api_key"))

	bad := configVolume(t, e)
	harness.Refuses(t, e, &harness.RunSpec{
		Image: image, Network: "none", Confine: harness.Contract(tmpfsSize),
		Env: map[string]string{whitelistVar: "sab, sab:8080"}, Mounts: []harness.Mount{bad},
	}, 1, "a whitelist with a port", `"sab:8080" has a port`)
	res, err := e.Run(t.Context(), t, &harness.RunSpec{
		Image: image, Network: "none", Confine: harness.Contract(tmpfsSize), Mounts: []harness.Mount{bad},
		Entrypoint: []string{python}, Args: []string{"-c", "import os; print(sorted(os.listdir('/config')))"},
	})
	if err != nil {
		t.Fatal(err)
	}
	harness.Equal(checks, "a refused start writes nothing", "[]\n", string(res.Stdout))
}

// TestHostnames is SABnzbd's own check of a request's Host, with no login
// set: the names SABNZBD_HOST_WHITELIST gives are served, and so is a
// Kubernetes service name, *.local, which SABnzbd always lets in; any
// other name is refused, where a page rebinding a name to the container
// would be.
func TestHostnames(t *testing.T) {
	t.Parallel()
	e, image := underTest(t)
	s, _ := serveFresh(t, e, image, map[string]string{whitelistVar: "sab,sab.example.com"})
	for host, want := range map[string]int{
		"sab":                      http.StatusOK,
		"sab.example.com:8080":     http.StatusOK,
		"sab.ns.svc.cluster.local": http.StatusOK,
		"other.example.com":        http.StatusForbidden,
	} {
		resp, err := s.Do(t.Context(), nil, http.MethodGet, versionPath, http.Header{"Host": {host}}, nil)
		harness.Answers(t, "Host "+host, want, resp, err)
	}
}
