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

package auth

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

func TestPrincipalFromContext(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		ctx  context.Context
		want string
	}{
		{"no subject", context.Background(), ""},
		{
			"authenticated subject yields its ID",
			SubjectIntoContext(context.Background(), interfaces.Subject{ID: "user-7", Username: "alice", Authenticated: true}),
			"user-7",
		},
		{
			"unauthenticated subject yields nothing",
			SubjectIntoContext(context.Background(), interfaces.Subject{ID: "user-7", Authenticated: false}),
			"",
		},
		{"system context carries no principal", NewSystemContext(context.Background()), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := PrincipalFromContext(tc.ctx); got != tc.want {
				t.Fatalf("PrincipalFromContext = %q, want %q", got, tc.want)
			}
		})
	}
}
