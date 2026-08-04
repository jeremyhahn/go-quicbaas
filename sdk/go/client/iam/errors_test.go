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

package iam

import (
	"encoding/json"
	"errors"
	"testing"
)

// TestUnknownEnumValueError_Error verifies the message format and that enum
// UnmarshalJSON implementations surface the typed error via errors.As.
func TestUnknownEnumValueError_Error(t *testing.T) {
	e := &UnknownEnumValueError{Enum: "consent status", Value: "nope"}
	if got, want := e.Error(), "unknown consent status: nope"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}

	var s ConsentStatus
	err := json.Unmarshal([]byte(`"nope"`), &s)
	var enumErr *UnknownEnumValueError
	if !errors.As(err, &enumErr) {
		t.Fatalf("expected *UnknownEnumValueError, got %T: %v", err, err)
	}
	if enumErr.Value != "nope" {
		t.Errorf("Value = %q, want %q", enumErr.Value, "nope")
	}
}

// TestUnknownEnumValueError_AllEnums verifies every GDPR enum returns the
// typed error for an unrecognized string value.
func TestUnknownEnumValueError_AllEnums(t *testing.T) {
	targets := []json.Unmarshaler{
		new(ConsentStatus),
		new(ExportFormat),
		new(ExportStatus),
		new(DeletionStatus),
	}
	for _, target := range targets {
		err := target.UnmarshalJSON([]byte(`"bogus-value"`))
		var enumErr *UnknownEnumValueError
		if !errors.As(err, &enumErr) {
			t.Errorf("%T: expected *UnknownEnumValueError, got %T: %v", target, err, err)
		}
	}
}

// TestJSONUnmarshalTypeError_Error verifies the message format and that
// structurally-invalid JSON surfaces the typed error.
func TestJSONUnmarshalTypeError_Error(t *testing.T) {
	e := &JSONUnmarshalTypeError{Target: "ConsentStatus", Data: "[1,2]"}
	if got, want := e.Error(), "cannot unmarshal [1,2] into ConsentStatus"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}

	var s ConsentStatus
	err := json.Unmarshal([]byte(`[1,2]`), &s)
	var typeErr *JSONUnmarshalTypeError
	if !errors.As(err, &typeErr) {
		t.Fatalf("expected *JSONUnmarshalTypeError, got %T: %v", err, err)
	}
	if typeErr.Target != "ConsentStatus" {
		t.Errorf("Target = %q, want %q", typeErr.Target, "ConsentStatus")
	}
}

// TestJSONUnmarshalTypeError_FlexInt verifies FlexInt returns the typed error
// for JSON that is neither number, numeric string, nor float.
func TestJSONUnmarshalTypeError_FlexInt(t *testing.T) {
	var f FlexInt
	err := json.Unmarshal([]byte(`{"x":1}`), &f)
	var typeErr *JSONUnmarshalTypeError
	if !errors.As(err, &typeErr) {
		t.Fatalf("expected *JSONUnmarshalTypeError, got %T: %v", err, err)
	}
	if typeErr.Target != "FlexInt" {
		t.Errorf("Target = %q, want %q", typeErr.Target, "FlexInt")
	}
}

// TestIntParseError_Error verifies the message format and that a non-numeric
// string value surfaces the typed error from FlexInt.
func TestIntParseError_Error(t *testing.T) {
	e := &IntParseError{Value: "abc"}
	if got, want := e.Error(), `cannot parse "abc" as integer`; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}

	var f FlexInt
	err := json.Unmarshal([]byte(`"abc"`), &f)
	var parseErr *IntParseError
	if !errors.As(err, &parseErr) {
		t.Fatalf("expected *IntParseError, got %T: %v", err, err)
	}
	if parseErr.Value != "abc" {
		t.Errorf("Value = %q, want %q", parseErr.Value, "abc")
	}
}

// TestIntParseError_ValidInputsDoNotError verifies the FlexInt happy paths
// (int, numeric string, float string, float) return no error.
func TestIntParseError_ValidInputsDoNotError(t *testing.T) {
	for _, in := range []string{`7`, `"7"`, `"1.0"`, `2.0`} {
		var f FlexInt
		if err := json.Unmarshal([]byte(in), &f); err != nil {
			t.Errorf("unmarshal %s: unexpected error %v", in, err)
		}
	}
}
