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

package orchestration

import (
	"errors"
	"strings"
	"testing"
)

// TestValidateProjectID_AcceptsAnIDOrNothing pins the two shapes that are
// legitimate: a UUID, and an empty value meaning unscoped.
func TestValidateProjectID_AcceptsAnIDOrNothing(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, projectID string }{
		{"unset means unscoped, which is a real state", ""},
		{"canonical lower case", "6f9619ff-8b86-d011-b42d-00c04fc964ff"},
		{"upper case is the same id", "6F9619FF-8B86-D011-B42D-00C04FC964FF"},
		{"the nil uuid is still a well-formed id", "00000000-0000-0000-0000-000000000000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := ValidateProjectID("project_id", tc.projectID); err != nil {
				t.Fatalf("ValidateProjectID(%q) = %v, want nil", tc.projectID, err)
			}
		})
	}
}

// TestValidateProjectID_RefusesAnythingElse is the invalid-input half. Every
// case is a value a caller could plausibly send believing it identifies a
// project, which is the point: the refusal is what tells them it does not.
func TestValidateProjectID_RefusesAnythingElse(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, projectID string }{
		{"the project's name", "go-quicbaas"},
		{"a slug with a slash", "acme/go-quicbaas"},
		{"a filesystem path", "/workspace/go-quicbaas"},
		{"a uuid with a non-hex character", "6f9619ff-8b86-d011-b42d-00c04fc964fg"},
		{"a truncated uuid", "6f9619ff-8b86-d011"},
		{"a uuid with surrounding whitespace", " 6f9619ff-8b86-d011-b42d-00c04fc964ff"},
		{"a single space", " "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateProjectID("project_id", tc.projectID)
			var invalid *InvalidProjectIDError
			if !errors.As(err, &invalid) {
				t.Fatalf("ValidateProjectID(%q) = %v, want InvalidProjectIDError", tc.projectID, err)
			}
			if !errors.Is(err, ErrInvalidProjectID) {
				t.Errorf("the refusal must unwrap to the sentinel: %v", err)
			}
			if invalid.ProjectID != tc.projectID || invalid.Field != "project_id" {
				t.Errorf("the refusal must name the field and the value: %+v", invalid)
			}
			// The message has to be actionable: the whole failure mode is a
			// caller who believes a name is an id.
			for _, want := range []string{tc.projectID, "project_id", "not its name"} {
				if !strings.Contains(invalid.Error(), want) {
					t.Errorf("Error() = %q, missing %q", invalid.Error(), want)
				}
			}
		})
	}
}

// TestInvalidProjectIDError_WithoutAField proves the message is still whole
// when the caller names no field, so a refusal raised from a path that has
// only the value does not read as a truncated sentence.
func TestInvalidProjectIDError_WithoutAField(t *testing.T) {
	t.Parallel()
	err := &InvalidProjectIDError{ProjectID: "go-quicbaas"}
	msg := err.Error()
	if !strings.Contains(msg, "go-quicbaas") || strings.Contains(msg, ": : ") {
		t.Fatalf("Error() = %q", msg)
	}
	if !errors.Is(err, ErrInvalidProjectID) {
		t.Fatal("Unwrap must reach the sentinel")
	}
}
