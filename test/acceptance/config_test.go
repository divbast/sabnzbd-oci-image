// SPDX-License-Identifier: GPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package acceptance

import (
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/divbast/ci/harness"
)

// TestFSGroupConfig is /config as Kubernetes leaves a volume given an
// fsGroup of 65532: root's, group 65532, setgid, group-writable. SABnzbd
// writes there.
func TestFSGroupConfig(t *testing.T) {
	t.Parallel()
	e, image := underTest(t)
	vol := configVolume(t, e)
	prepare(t, e, image, vol, "import os; os.chown('/config', 0, 65532); os.chmod('/config', 0o2775)")
	s := serveSAB(t, e, image, nil, vol)
	_, err := readINI(t, s)
	harness.NewChecks(t).True("SABnzbd wrote its settings there", err == nil, errString(err))
	healthy(t, s, "the shipped probe passes")
}

// TestRefusesRootOwnedConfig is a /config only root can write: the start
// stops at once, naming it.
func TestRefusesRootOwnedConfig(t *testing.T) {
	t.Parallel()
	e, image := underTest(t)
	vol := configVolume(t, e)
	prepare(t, e, image, vol, "import os; os.chown('/config', 0, 0); os.chmod('/config', 0o755)")
	harness.Refuses(t, e, &harness.RunSpec{
		Image: image, Network: "none", Confine: harness.Contract(tmpfsSize), Mounts: []harness.Mount{vol},
	}, 1, "a root-owned /config", "cannot write the first settings in /config")
}

// TestRefusesNoConfigMount is no /config at all: the image's own is on the
// read-only root, so the start stops at once, naming it.
func TestRefusesNoConfigMount(t *testing.T) {
	t.Parallel()
	e, image := underTest(t)
	harness.Refuses(t, e, &harness.RunSpec{
		Image: image, Network: "none", Confine: harness.Contract(tmpfsSize),
	}, 1, "no /config mount", "/config, which must be a directory uid 65532 can write")
}

// linuxserverINI is a settings file as another image leaves it: its own
// host and port, download folders at its own paths, and a whitelist.
const linuxserverINI = `[misc]
host = 0.0.0.0
port = 8085
host_whitelist = mine.example.com,
download_dir = /incomplete-downloads
complete_dir = /downloads
`

// writeINI is the Python prepare runs to write linuxserverINI, owned by uid.
func writeINI(uid string) string {
	return "import os\nopen('/config/sabnzbd.ini', 'w').write('''" + linuxserverINI + "''')\n" +
		"for p in ('/config', '/config/sabnzbd.ini', '/config/.prepared'): os.chown(p, " + uid + ", " + uid + ")\n" +
		"os.chmod('/config/sabnzbd.ini', 0o644)\n"
}

// TestRefusesForeignOwnedConfig is a /config another image left owned by
// uid 1000, with its settings in it: the start stops within a minute,
// naming /config and the chown that fixes it, rather than SABnzbd's own
// traceback.
func TestRefusesForeignOwnedConfig(t *testing.T) {
	t.Parallel()
	e, image := underTest(t)
	vol := configVolume(t, e)
	prepare(t, e, image, vol, writeINI("1000"))
	harness.Refuses(t, e, &harness.RunSpec{
		Image: image, Network: "none", Confine: harness.Contract(tmpfsSize), Mounts: []harness.Mount{vol},
	}, 1, "a uid-1000 /config", "SABnzbd cannot write /config: give it, and everything in it, to uid 65532")
}

// TestMigratedIni is another image's settings, once their directory is
// given to uid 65532: the command leaves them alone, whatever the
// variable says, SABnzbd serves on 8080 whatever port they name, since the
// command line wins, and download folders it cannot make are SABnzbd's own
// warning, not a stop.
func TestMigratedIni(t *testing.T) {
	t.Parallel()
	e, image := underTest(t)
	vol := configVolume(t, e)
	prepare(t, e, image, vol, writeINI("65532"))
	s := serveSAB(t, e, image, map[string]string{whitelistVar: "other.example.com"}, vol)
	checks := harness.NewChecks(t)
	ini, err := readINI(t, s)
	if err != nil {
		t.Fatal(err)
	}
	harness.Equal(checks, "the whitelist is theirs", "mine.example.com,", iniValue(ini, "host_whitelist"))
	harness.Equal(checks, "their download folder stands", "/incomplete-downloads", iniValue(ini, "download_dir"))
	healthy(t, s, "SABnzbd serves on 8080, healthy")
	// A folder SABnzbd cannot create is its own warning in the web UI
	var got struct {
		Warnings []struct{ Text string } `json:"warnings"`
	}
	api(t, s, apiKey(t, s), url.Values{"mode": {"warnings"}}, &got)
	texts := make([]string, 0, len(got.Warnings))
	for _, w := range got.Warnings {
		texts = append(texts, w.Text)
	}
	checks.True("SABnzbd warns it cannot create their download folder",
		slices.ContainsFunc(texts, func(s string) bool {
			return strings.Contains(s, "Cannot create directory") && strings.Contains(s, "downloads")
		}),
		harness.Clip(strings.Join(texts, " | ")))
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
