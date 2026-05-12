// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package config

// XKMSConfig configures how the QuicBaaS service reaches an xkms
// backend. The field set is intentionally minimal — it drives the
// transport selection for the xkms SSH SDK client (`xkmsssh.Client`)
// used by the agent deploy service and the REST/Wails/CLI surfaces.
//
// When URL is empty the service falls back to the embedded transport
// with a file-backed store rooted under `$DataDir/xkms-ssh` so the
// zero-config deployment keeps working.
type XKMSConfig struct {
	// URL is the base URL of a remote xkmsd instance. When non-empty
	// the REST transport is used. Example:
	//   https://xkmsd.example.com:8443
	URL string `mapstructure:"url"`

	// Token is an optional bearer token forwarded with every REST
	// request to xkmsd.
	Token string `mapstructure:"token"`
}
