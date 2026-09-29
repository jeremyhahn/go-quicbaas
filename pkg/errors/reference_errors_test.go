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

package errors

import (
	stderrors "errors"
	"testing"
)

func TestReferenceNotFoundError_NamesFieldKindAndRef(t *testing.T) {
	err := &ReferenceNotFoundError{Field: "forge.token_secret_ref", Kind: ReferenceKindSecret, Ref: "gitea-token"}
	want := `forge.token_secret_ref: secret "gitea-token" does not exist`
	if got := err.Error(); got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestReferenceUnavailableError(t *testing.T) {
	cause := stderrors.New("barrier sealed")
	tests := []struct {
		name string
		err  *ReferenceUnavailableError
		want string
	}{
		{
			name: "store failure",
			err:  &ReferenceUnavailableError{Field: "default_workflow", Kind: ReferenceKindWorkflow, Ref: "sdlc", Err: cause},
			want: `default_workflow: workflow "sdlc" cannot be verified: barrier sealed`,
		},
		{
			name: "no store attached",
			err:  &ReferenceUnavailableError{Field: "host_affinity", Kind: ReferenceKindFleetNode, Ref: "node-1"},
			want: `host_affinity: fleet node "node-1" cannot be verified: no fleet node store is attached`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Fatalf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReferenceUnavailableError_UnwrapsTheStoreFailure(t *testing.T) {
	cause := stderrors.New("barrier sealed")
	err := error(&ReferenceUnavailableError{Field: "f", Kind: ReferenceKindSecret, Ref: "r", Err: cause})
	if !stderrors.Is(err, cause) {
		t.Fatal("the store failure must stay reachable with errors.Is")
	}
	var notFound *ReferenceNotFoundError
	if stderrors.As(err, &notFound) {
		t.Fatal("an unavailable store must never read as a missing reference")
	}
}
