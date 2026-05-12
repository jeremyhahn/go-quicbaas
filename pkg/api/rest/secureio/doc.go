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

// Package secureio provides JSON-wire field types that decode
// sensitive payloads (passwords, passphrases, tokens) directly into
// mlocked buffers supplied by the xkms secure SDK. Plaintext bytes
// never land in a Go string on the unmarshal path, so the Go GC never
// has a second copy and the original JSON bytes remain the only
// caller-visible plaintext — callers are expected to wipe those
// immediately after the unmarshal completes.
//
// The package lives under pkg/api/rest/ for historical reasons but
// does not depend on REST — any JSON-framed wire (REST, QUIC, MCP)
// may import it safely.
//
// The canonical field type is PasswordField, used by the IAM login
// handler. Additional fields should follow the same shape: accept a
// JSON string token, strip the quotes without converting to string,
// and decode (base64 or raw) directly into the buffer.
package secureio
