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

package errors

import (
	"errors"
	"fmt"
	"testing"
)

func TestEmbeddedIdentityRequired_IsIdentifiedThroughWrapping(t *testing.T) {
	var required error = NewEmbeddedIdentityRequired("NewClient")
	err := fmt.Errorf("client: %w", required)

	if !IsEmbeddedIdentityRequired(err) {
		t.Fatal("IsEmbeddedIdentityRequired did not find the wrapped error")
	}
	if !errors.Is(err, &EmbeddedIdentityRequiredError{}) {
		t.Error("errors.Is did not match the type")
	}
	if !IsInvalidInput(err) {
		t.Error("the error is not classified as invalid input")
	}
}

func TestEmbeddedIdentityRequired_OtherErrorsAreNot(t *testing.T) {
	if IsEmbeddedIdentityRequired(NewInvalidInput("NewClient", "address cannot be empty")) {
		t.Error("a plain invalid-input error was taken for EmbeddedIdentityRequiredError")
	}
	if IsEmbeddedIdentityRequired(nil) {
		t.Error("nil was taken for EmbeddedIdentityRequiredError")
	}
}
