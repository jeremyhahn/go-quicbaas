// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

//go:build !unix

package workerimage

// fileOwner has no meaning off unix: the worker image is a linux image built
// and run by a unix engine, and a host without uids cannot hold the property
// the bind-mount probe checks.
func fileOwner(string) (int, error) {
	return 0, &UnsupportedPlatformError{What: "file ownership"}
}
