//go:build quic && !iam

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

package quic

// getIAMQRDBPermission is a no-op when IAM is not compiled in: there are no IAM
// opcodes to map.
func getIAMQRDBPermission(_ byte) (resource, action string, handled bool) {
	return "", "", false
}
