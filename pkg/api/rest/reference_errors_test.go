//go:build rest

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

package rest

import (
	"encoding/json"
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"testing"

	quicbaaserrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// Every responder a reference-checked save answers through maps the two
// reference errors the same way, with the reference error's own message even
// when it wraps a store error of its own.
func TestReferenceErrorsMapOnEveryResponder(t *testing.T) {
	sealed := &quicbaaserrors.QBError{Code: quicbaaserrors.ErrInternal, Op: "secrets.GetSecret", Err: stderrors.New("barrier sealed")}
	errs := []struct {
		name    string
		err     error
		status  int
		code    string
		message string
	}{
		{"not found", &quicbaaserrors.ReferenceNotFoundError{Field: "forge.token_secret_ref", Kind: quicbaaserrors.ReferenceKindSecret, Ref: "tok"},
			http.StatusBadRequest, referenceNotFoundCode, `forge.token_secret_ref: secret "tok" does not exist`},
		{"unavailable", &quicbaaserrors.ReferenceUnavailableError{Field: "forge.token_secret_ref", Kind: quicbaaserrors.ReferenceKindSecret, Ref: "tok", Err: sealed},
			http.StatusServiceUnavailable, referenceUnavailableCode, `forge.token_secret_ref: secret "tok" cannot be verified: ` + sealed.Error()},
	}
	responders := map[string]func(http.ResponseWriter, *http.Request, error){
		"sdlc":         respondSDLCError,
		"project":      handleProjectError,
		"orchestrator": handleOrchestratorError,
	}
	for responderName, respond := range responders {
		for _, tc := range errs {
			t.Run(responderName+"/"+tc.name, func(t *testing.T) {
				w := httptest.NewRecorder()
				respond(w, httptest.NewRequest(http.MethodPost, "/", nil), tc.err)
				if w.Code != tc.status {
					t.Fatalf("status = %d, want %d", w.Code, tc.status)
				}
				var body ErrorResponse
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
					t.Fatalf("decode: %v", err)
				}
				if body.Code != tc.code || body.Error != tc.message {
					t.Fatalf("body = %+v, want code %q message %q", body, tc.code, tc.message)
				}
			})
		}
	}
}

// Anything else falls through to the responder's own mapping.
func TestRespondReferenceError_IgnoresOtherErrors(t *testing.T) {
	w := httptest.NewRecorder()
	if respondReferenceError(w, httptest.NewRequest(http.MethodPost, "/", nil), &service.ProjectInvalidError{Field: "name"}) {
		t.Fatal("a non-reference error was answered as one")
	}
	if w.Body.Len() != 0 {
		t.Fatal("a non-reference error wrote a response")
	}
}
