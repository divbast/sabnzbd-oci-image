// SPDX-License-Identifier: GPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package acceptance

import (
	"bytes"
	"encoding/base64"
	bin "encoding/binary"
	"hash/crc32"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/divbast/ci/harness"
)

// TestTools is what SABnzbd finds of the tools it runs, as its start log
// reports them: the unrar the image built, RARLAB's own and new enough
// for SABnzbd, 7zz, and sabctools, its par2 and yEnc module, linked to
// OpenSSL. SABnzbd raises a warning when any is missing or too old, and
// none is raised.
func TestTools(t *testing.T) {
	t.Parallel()
	e, image := underTest(t)
	s, _ := serveFresh(t, e, image, nil)
	for _, line := range []string{
		"UNRAR binary... found (/usr/local/bin/unrar)",
		"7za binary... found (/usr/local/bin/7zz)",
		"SABCTools module is linked to OpenSSL: True",
	} {
		harness.LogContains(t, s.C, "SABnzbd's log", line)
	}
	checks := harness.NewChecks(t)
	log, err := s.C.Logs(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`UNRAR binary version (\d+)\.(\d+)`).FindStringSubmatch(log)
	if m == nil {
		checks.True("SABnzbd reports unrar's version", false, harness.Clip(log))
	} else {
		major, _ := strconv.Atoi(m[1])
		minor, _ := strconv.Atoi(m[2])
		checks.True("unrar is 5.50 or newer, as SABnzbd needs", major > 5 || major == 5 && minor >= 50, m[0])
	}
	var warnings struct {
		Warnings []struct{ Text string }
	}
	api(t, s, apiKey(t, s), url.Values{"mode": {"warnings"}}, &warnings)
	for _, w := range warnings.Warnings {
		lower := strings.ToLower(w.Text)
		checks.True("no warning about a tool", !strings.Contains(lower, "unrar") && !strings.Contains(lower, "7z") && !strings.Contains(lower, "sabctools"), w.Text)
	}
}

// rar4 is a RAR 4 archive holding name with content, stored, as the format
// RARLAB documents it: the marker, the archive header, one file header and
// its data, and the end of archive, each header's CRC the low half of the
// CRC-32 of its bytes after the CRC itself. unrar checks the data's CRC-32
// too, so a test of it proves unrar read the file whole.
func rar4(name string, content []byte) []byte {
	var out bytes.Buffer
	out.Write([]byte{0x52, 0x61, 0x72, 0x21, 0x1A, 0x07, 0x00})
	block := func(b []byte) {
		var crc [2]byte
		bin.LittleEndian.PutUint16(crc[:], uint16(crc32.ChecksumIEEE(b))) //nolint:gosec // G115: the format keeps the CRC's low half
		out.Write(crc[:])
		out.Write(b)
	}
	// The archive header: type, flags, size 13, two reserved fields
	block([]byte{0x73, 0x00, 0x00, 13, 0x00, 0, 0, 0, 0, 0, 0})
	var h bytes.Buffer
	h.WriteByte(0x74)
	le := func(v any) { _ = bin.Write(&h, bin.LittleEndian, v) }
	le(uint16(0x8000))         // a long block, as every file header is
	le(uint16(32 + len(name))) //nolint:gosec // G115: a header of a few dozen bytes; its size
	le(uint32(len(content)))   //nolint:gosec // G115: a fixture of a few bytes; packed
	le(uint32(len(content)))   //nolint:gosec // G115: unpacked, the same
	h.WriteByte(3)             // made on Unix
	le(crc32.ChecksumIEEE(content))
	le(uint32(0x5B7A6000)) // a DOS time, 2025-11-26
	h.WriteByte(20)        // the version needed to unpack it
	h.WriteByte(0x30)      // stored
	le(uint16(len(name)))  //nolint:gosec // G115: a short fixed name
	le(uint32(0o100644))   // a regular file, mode 0644
	h.WriteString(name)
	block(h.Bytes())
	out.Write(content)
	block([]byte{0x7B, 0x00, 0x40, 0x07, 0x00})
	return out.Bytes()
}

// TestUnpacks has the image's unrar and 7zz test archives, inside the
// image, under the contract: a RAR this test writes, and a 7z the image's
// own 7zz makes. SABnzbd unpacks a job only once its articles are
// downloaded, which needs a news server, so this is the tools alone.
func TestUnpacks(t *testing.T) {
	t.Parallel()
	e, image := underTest(t)
	archive := base64.StdEncoding.EncodeToString(rar4("divbast.txt", []byte("unpacked by the image's unrar\n")))
	code := `import base64, subprocess, sys
open('/tmp/t.rar', 'wb').write(base64.b64decode(sys.argv[1]))
for argv in (['/usr/local/bin/unrar', 't', '/tmp/t.rar'],
             ['/usr/local/bin/7zz', 'a', '/tmp/t.7z', '/app/sabnzbd/SABnzbd.py'],
             ['/usr/local/bin/7zz', 't', '/tmp/t.7z']):
    r = subprocess.run(argv, capture_output=True, text=True)
    ok = 'All OK' in r.stdout or 'Everything is Ok' in r.stdout
    print(' '.join(argv[:2]), r.returncode, 'ok' if ok else r.stdout + r.stderr)
`
	res, err := e.Run(t.Context(), t, &harness.RunSpec{
		Image: image, Network: "none", Confine: harness.Contract(tmpfsSize),
		Entrypoint: []string{python}, Args: []string{"-c", code, archive},
	})
	if err != nil {
		t.Fatal(err)
	}
	checks := harness.NewChecks(t)
	out := string(res.Stdout)
	checks.True("unrar tests the RAR: All OK", strings.Contains(out, "/usr/local/bin/unrar t 0 ok"), out+string(res.Stderr))
	checks.True("7zz makes a 7z", strings.Contains(out, "/usr/local/bin/7zz a 0 ok"), out)
	checks.True("7zz tests it: Everything is Ok", strings.Contains(out, "/usr/local/bin/7zz t 0 ok"), out)
}

// udpSent counts the UDP datagrams the container's network sends over its
// first argument's seconds, from the kernel's own counter: SABnzbd sends
// each SSDP announcement from a socket it closes at once, which a look at
// the socket tables would miss.
const udpSent = `import sys, time
def sent():
    lines = [l.split() for l in open('/proc/net/snmp').read().splitlines() if l.startswith('Udp:')]
    return int(lines[1][lines[0].index('OutDatagrams')])
before = sent()
time.sleep(int(sys.argv[1]))
print(sent() - before)
`

// TestNoSSDP holds SSDP off over 35 seconds, past two of SABnzbd's 15 s
// announcements, which SABnzbd sends with SSDP on: the container sends no
// UDP datagram at all.
func TestNoSSDP(t *testing.T) {
	t.Parallel()
	e, image := underTest(t)
	s, _ := serveFresh(t, e, image, nil)
	const watch = 35 * time.Second
	res, err := s.C.Exec(t.Context(), nil, python, "-c", udpSent, strconv.Itoa(int(watch/time.Second)))
	if err != nil {
		t.Fatal(err)
	}
	harness.Equal(harness.NewChecks(t), "no UDP datagram sent in "+watch.String(), "0", strings.TrimSpace(string(res.Stdout)+string(res.Stderr)))
}
