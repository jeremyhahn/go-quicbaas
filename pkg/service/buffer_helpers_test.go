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

package service

import (
	"testing"

	secure "github.com/jeremyhahn/go-xkms/sdk/go/secure"
)

// mustPasswordBuffer builds an mlocked *secure.Buffer for use in test
// LoginRequest literals. The buffer ownership transfers to the callee
// (service.Login -> AuthManager.Authenticate), which destroys the
// buffer on every return path.
//
// Empty input returns nil to match the production contract where a nil
// Buffer signals "no password supplied".
func mustPasswordBuffer(t *testing.T, password string) *secure.Buffer {
	t.Helper()
	if password == "" {
		return nil
	}
	buf, err := secure.NewBufferFromBytes([]byte(password))
	if err != nil {
		t.Fatalf("mustPasswordBuffer: %v", err)
	}
	return buf
}
