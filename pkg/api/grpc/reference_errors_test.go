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

package grpc

import (
	stderrors "errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	quicbaaserrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// Every converter a reference-checked save answers through maps the two
// reference errors the same way, with the reference error's own message.
func TestReferenceErrorsMapOnEveryConverter(t *testing.T) {
	sealed := stderrors.New("barrier sealed")
	errs := []struct {
		name    string
		err     error
		code    codes.Code
		message string
	}{
		{"not found", &quicbaaserrors.ReferenceNotFoundError{Field: "default_workflow", Kind: quicbaaserrors.ReferenceKindWorkflow, Ref: "sdlc"},
			codes.InvalidArgument, `default_workflow: workflow "sdlc" does not exist`},
		{"unavailable", &quicbaaserrors.ReferenceUnavailableError{Field: "tenant_id", Kind: quicbaaserrors.ReferenceKindTenant, Ref: "acme", Err: sealed},
			codes.Unavailable, `tenant_id: tenant "acme" cannot be verified: barrier sealed`},
	}
	converters := map[string]func(error) error{
		"sdlc":         convertSDLCError,
		"project":      convertProjectError,
		"orchestrator": convertOrchestratorError,
	}
	for converterName, convert := range converters {
		for _, tc := range errs {
			t.Run(converterName+"/"+tc.name, func(t *testing.T) {
				st, ok := status.FromError(convert(tc.err))
				if !ok || st.Code() != tc.code || st.Message() != tc.message {
					t.Fatalf("status = %v, want %v %q", st, tc.code, tc.message)
				}
			})
		}
	}
}

func TestReferenceStatus_IgnoresOtherErrors(t *testing.T) {
	if _, ok := referenceStatus(&service.ProjectInvalidError{Field: "name"}); ok {
		t.Fatal("a non-reference error was mapped as one")
	}
}
