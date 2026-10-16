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
	"strings"
	"testing"
)

func TestPromptTransportUnsupported_NamesTheTransportAndClassifies(t *testing.T) {
	err := NewPromptTransportUnsupported("prompt.List", "*fake.Transport")
	if err.Transport != "*fake.Transport" {
		t.Fatalf("Transport = %q", err.Transport)
	}
	if !strings.Contains(err.Error(), "*fake.Transport") || !strings.Contains(err.Error(), "Prompts") {
		t.Fatalf("message does not name the transport and service: %q", err.Error())
	}
	if !IsUnimplemented(err) {
		t.Fatal("the refusal must classify as Unimplemented")
	}
	var wrapped error = err
	if !stderrors.Is(wrapped, &PromptTransportUnsupportedError{}) {
		t.Fatal("errors.Is must match the type")
	}
	var qb *QBError
	if !stderrors.As(wrapped, &qb) || qb.Code != CodeUnimplemented {
		t.Fatalf("errors.As must reach the QBError: %+v", qb)
	}
}

func TestPromptTransportUnsupported_DoesNotMatchOtherErrors(t *testing.T) {
	err := NewPromptTransportUnsupported("prompt.Get", "*fake.Transport")
	if err.Is(NewSDLCTransportUnsupported("sdlc.Binding", "*fake.Transport")) {
		t.Fatal("a prompt refusal must not match the SDLC refusal")
	}
	if stderrors.Is(NewInvalidInput("prompt.Get", "x"), &PromptTransportUnsupportedError{}) {
		t.Fatal("an unrelated error must not match the prompt refusal")
	}
}
