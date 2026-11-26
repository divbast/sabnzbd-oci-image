// SPDX-License-Identifier: GPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package pins

import (
	"context"
	"net/http"
	"time"
)

// UnrarSHA256 fetches unrarsrc-<version>.tar.gz from RARLAB, at base,
// https://www.rarlab.com, and returns its sha256. RARLAB publishes no
// checksum, so this is the checksum of what it serves now, which the
// image's build then holds the tarball to.
func UnrarSHA256(ctx context.Context, client *http.Client, base, version string) (string, error) {
	return unrarSHA256(ctx, client, base, version, nil)
}

// unrarSHA256 is UnrarSHA256, with the waits between tries delays.
func unrarSHA256(ctx context.Context, client *http.Client, base, version string, delays []time.Duration) (string, error) {
	body, err := get(ctx, client, base+"/rar/unrarsrc-"+version+".tar.gz", delays)
	if err != nil {
		return "", err
	}
	return sha256Of(body), nil
}
