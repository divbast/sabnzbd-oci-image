// SPDX-License-Identifier: GPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package probe

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"os"
	"slices"
	"strings"
)

// listen is the state /proc/net/tcp and tcp6 give a listening socket.
const listen = "0A"

// listening returns nil when a socket listens on port on every address of
// the network namespace that proc's net/tcp6 and net/tcp describe: ::, or
// 0.0.0.0 on a kernel with no IPv6. SABnzbd listens where its settings say
// once it restarts itself, so a restored backup that names 127.0.0.1 leaves
// it answering inside the container alone; the error then says where port
// is listened on, if anywhere.
func listening(proc string, port int) error {
	root, err := os.OpenRoot(proc)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }() // read-only, so nothing to lose
	want := fmt.Sprintf("%04X", port)
	var elsewhere []string
	for _, name := range []string{"net/tcp6", "net/tcp"} {
		data, err := root.ReadFile(name)
		if errors.Is(err, fs.ErrNotExist) && name == "net/tcp6" {
			continue // a kernel with no IPv6
		}
		if err != nil {
			return err
		}
		lines := strings.Split(string(data), "\n")
		for _, line := range lines[1:] {
			f := strings.Fields(line)
			if len(f) < 4 || f[3] != listen {
				continue
			}
			addr, p, ok := strings.Cut(f[1], ":")
			if !ok || !strings.EqualFold(p, want) {
				continue
			}
			if strings.Trim(addr, "0") == "" {
				return nil
			}
			elsewhere = append(elsewhere, address(addr))
		}
	}
	if len(elsewhere) == 0 {
		return fmt.Errorf("nothing listens on port %d", port)
	}
	slices.Sort(elsewhere)
	return fmt.Errorf("SABnzbd listens on port %d at %s alone, not on every address, so nothing outside the container reaches it: "+
		"restart the container, which starts SABnzbd on every address again", port, strings.Join(slices.Compact(elsewhere), ", "))
}

// address reads an address as /proc/net/tcp and tcp6 write it: in hex, in
// 32-bit words each in the host's order, which is little-endian on every
// architecture the image is built for.
func address(h string) string {
	b, err := hex.DecodeString(h)
	if err != nil || (len(b) != 4 && len(b) != 16) {
		return h
	}
	for i := 0; i < len(b); i += 4 {
		slices.Reverse(b[i : i+4])
	}
	a, _ := netip.AddrFromSlice(b) // 4 or 16 bytes, as checked
	return a.String()
}
