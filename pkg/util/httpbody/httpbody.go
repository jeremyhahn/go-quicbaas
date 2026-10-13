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

// Package httpbody releases HTTP response bodies so the transport can reuse
// the underlying connection.
package httpbody

import (
	"errors"
	"io"
)

// DrainClose reads body to EOF and closes it. Draining lets net/http return
// the connection to its idle pool. Both the read and the close errors are
// returned, joined, so the caller can log them with its own logger.
func DrainClose(body io.ReadCloser) error {
	_, drainErr := io.Copy(io.Discard, body)
	return errors.Join(drainErr, body.Close())
}
