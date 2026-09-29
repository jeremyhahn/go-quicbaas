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

package sshstore

import (
	"errors"
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestErrorMessages pins what an operator reads in the boot log: every error
// names the record or path it is about, and wrapping errors expose their cause.
func TestErrorMessages(t *testing.T) {
	cause := errors.New("disk on fire")
	tests := []struct {
		name  string
		err   error
		want  []string
		cause error
	}{
		{name: "nil input", err: &NilInputError{Field: "sealed"}, want: []string{"sealed is required"}},
		{name: "legacy store", err: &LegacyStoreError{Op: "scan", Path: "/d/xkms-ssh", Err: cause}, want: []string{"scan", "/d/xkms-ssh", "disk on fire"}, cause: cause},
		{name: "sealed store", err: &SealedStoreError{Op: "put", Key: "ssh/keys/user/a/k", Err: cause}, want: []string{"put", "ssh/keys/user/a/k"}, cause: cause},
		{name: "conflict", err: &RecordConflictError{Key: "ssh/keys/user/a/k"}, want: []string{"different record", "ssh/keys/user/a/k"}},
		{name: "mismatch", err: &RecordMismatchError{Key: "ssh/keys/user/a/k"}, want: []string{"read-back", "ssh/keys/user/a/k"}},
		{name: "key verification with cause", err: &KeyVerificationError{Key: "user/a/k", Reason: "sign", Err: cause}, want: []string{"user/a/k", "sign", "disk on fire"}, cause: cause},
		{name: "key verification without cause", err: &KeyVerificationError{Key: "user/a/k", Reason: "differs"}, want: []string{"user/a/k", "differs"}},
		{name: "unparsed keys", err: &UnparsedKeysError{Records: 3, Parsed: 2}, want: []string{"3 legacy key records", "only 2"}},
		{name: "unexpected entry", err: &UnexpectedEntryError{Path: "/d/link", Mode: fs.ModeSymlink}, want: []string{"/d/link", "L"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, part := range tt.want {
				assert.Contains(t, tt.err.Error(), part)
			}
			if tt.cause != nil {
				assert.ErrorIs(t, tt.err, tt.cause)
			}
		})
	}
}
