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

package secrets

import (
	"bytes"
	"context"
	stderrors "errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// These tests never copy lent material out to assert on it
// (testing-guidelines.md): fn compares the borrowed slice in place and
// records only a verdict. Wiping is observed on the transport payload the
// test owns, not on protected storage.

// payloadTransport answers GetData with a response holding payload, so the
// test keeps a reference to the exact buffer the client must wipe.
func payloadTransport(payload []byte, gotMethod *string, gotReq **getSecretRequest) *mockTransport {
	return &mockTransport{
		requestFn: func(_ context.Context, svc, method string, req interface{}) (interface{}, error) {
			if gotMethod != nil {
				*gotMethod = svc + "/" + method
			}
			if gotReq != nil {
				*gotReq, _ = req.(*getSecretRequest)
			}
			return &service.GetSecretResponse{SecretID: "p", Version: 1, Data: payload}, nil
		},
	}
}

func allZero(b []byte) bool {
	for _, c := range b {
		if c != 0 {
			return false
		}
	}
	return true
}

func errorCode(t *testing.T, err error) errors.ErrorCode {
	t.Helper()
	var qb *errors.QBError
	if !stderrors.As(err, &qb) {
		t.Fatalf("error %v (%T) is not a *errors.QBError", err, err)
	}
	return qb.Code
}

func TestGetWithBytes_LendsFieldAndWipesPayload(t *testing.T) {
	payload := []byte(`{"username":"admin","password":"s3cr3t-value"}`)
	var method string
	var req *getSecretRequest
	c := NewClient(payloadTransport(payload, &method, &req))

	calls := 0
	matched := false
	payloadWipedBeforeBorrow := false
	err := c.GetWithBytes(context.Background(), "db/prod", "password", func(b []byte) error {
		calls++
		matched = bytes.Equal(b, []byte("s3cr3t-value"))
		payloadWipedBeforeBorrow = allZero(payload)
		return nil
	}, WithVersion(3))
	if err != nil {
		t.Fatalf("GetWithBytes: %v", err)
	}
	if calls != 1 || !matched {
		t.Fatalf("fn calls=%d matched=%v, want one call lending the field", calls, matched)
	}
	if !payloadWipedBeforeBorrow {
		t.Error("transport payload still held plaintext while fn ran; the lent bytes must come from protected storage")
	}
	if !allZero(payload) {
		t.Error("transport payload not wiped after GetWithBytes returned")
	}
	if method != serviceName+"/"+getDataMethod {
		t.Errorf("wire verb = %q, want %q", method, serviceName+"/"+getDataMethod)
	}
	if req == nil || req.Path != "db/prod" || req.Version != 3 {
		t.Errorf("request = %+v, want path db/prod version 3", req)
	}
}

func TestGetWithBytes_EmptyFieldLendsWholePayload(t *testing.T) {
	payload := []byte{0x00, 0xff, 0x10, 0x7f, '"'}
	want := append([]byte(nil), payload...)
	c := NewClient(payloadTransport(payload, nil, nil))

	matched := false
	if err := c.GetWithBytes(context.Background(), "raw", "", func(b []byte) error {
		matched = bytes.Equal(b, want)
		return nil
	}); err != nil {
		t.Fatalf("GetWithBytes: %v", err)
	}
	if !matched {
		t.Error("empty field did not lend the whole payload")
	}
	if !allZero(payload) {
		t.Error("payload not wiped")
	}
}

func TestGetWithBytes_UnescapesJSONString(t *testing.T) {
	cases := []struct {
		name, json string
		want       []byte
	}{
		{"simple escapes", `{"k":"a\"b\\c\/d\b\f\n\r\t"}`, []byte("a\"b\\c/d\b\f\n\r\t")},
		{"bmp escape", `{"k":"caf\u00e9"}`, []byte("café")},
		{"surrogate pair", `{"k":"\ud83d\ude00"}`, []byte("😀")},
		{"lone surrogate", `{"k":"x\ud800y"}`, []byte("x\uFFFDy")},
		{"uppercase hex", `{"k":"caf\u00E9"}`, []byte("café")},
		{"high surrogate then plain escape", `{"k":"\uD83D\u0041"}`, []byte("\uFFFDA")},
		{"high surrogate at end", `{"k":"a\ud83d"}`, []byte("a\uFFFD")},
		{"raw utf8", `{"k":"ключ"}`, []byte("ключ")},
		{"escaped key", `{"\u006b":"v"}`, []byte("v")},
		{"last duplicate wins", `{"k":"first","k":"second"}`, []byte("second")},
		{"nested siblings skipped", ` { "a" : {"k":"no","x":[1,"}",{"y":null}]} , "b":true, "n": -1.5e3 ,"k" : "yes" } `, []byte("yes")},
		{"empty string", `{"k":""}`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := []byte(tc.json)
			c := NewClient(payloadTransport(payload, nil, nil))
			matched := false
			if err := c.GetWithBytes(context.Background(), "p", "k", func(b []byte) error {
				matched = bytes.Equal(b, tc.want)
				return nil
			}); err != nil {
				t.Fatalf("GetWithBytes: %v", err)
			}
			if !matched {
				t.Error("lent bytes differ from the unescaped value")
			}
			if !allZero(payload) {
				t.Error("payload not wiped")
			}
		})
	}
}

func TestGetWithBytes_FieldErrorsWipePayload(t *testing.T) {
	cases := []struct {
		name, json, field string
		code              errors.ErrorCode
	}{
		{"missing field", `{"a":"b"}`, "k", errors.CodeNotFound},
		{"empty object", `{}`, "k", errors.CodeNotFound},
		{"non-string field", `{"k":42}`, "k", errors.CodeInvalidInput},
		{"object field", `{"k":{"x":"y"}}`, "k", errors.CodeInvalidInput},
		{"not json", `s3cr3t`, "k", errors.CodePreconditionFailed},
		{"json array", `["k"]`, "k", errors.CodePreconditionFailed},
		{"json string", `"k"`, "k", errors.CodePreconditionFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := []byte(tc.json)
			c := NewClient(payloadTransport(payload, nil, nil))
			called := false
			err := c.GetWithBytes(context.Background(), "p", tc.field, func([]byte) error {
				called = true
				return nil
			})
			if err == nil {
				t.Fatal("expected an error")
			}
			if got := errorCode(t, err); got != tc.code {
				t.Errorf("code = %v, want %v", got, tc.code)
			}
			if called {
				t.Error("fn must not run when the field cannot be lent")
			}
			if bytes.Contains([]byte(err.Error()), []byte("s3cr3t")) {
				t.Error("error message quotes the stored payload")
			}
			if !allZero(payload) {
				t.Error("payload not wiped on the error path")
			}
		})
	}
}

func TestGetWithBytes_FnErrorPropagatesAndWipes(t *testing.T) {
	payload := []byte(`{"k":"v"}`)
	c := NewClient(payloadTransport(payload, nil, nil))
	sentinel := stderrors.New("install failed")
	err := c.GetWithBytes(context.Background(), "p", "k", func([]byte) error { return sentinel })
	if !stderrors.Is(err, sentinel) {
		t.Fatalf("err = %v, want fn's error", err)
	}
	if !allZero(payload) {
		t.Error("payload not wiped after fn failed")
	}
}

func TestGetWithBytes_Validation(t *testing.T) {
	noop := func([]byte) error { return nil }
	c := NewClient(payloadTransport([]byte(`{"k":"v"}`), nil, nil))
	cases := []struct {
		name string
		ctx  context.Context
		path string
		fn   func([]byte) error
	}{
		{"empty path", context.Background(), "", noop},
		{"nil fn", context.Background(), "p", nil},
		{"nil ctx", nil, "p", noop}, //nolint:staticcheck // SA1012: nil context is the case under test
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := c.GetWithBytes(tc.ctx, tc.path, "k", tc.fn)
			if got := errorCode(t, err); got != errors.CodeInvalidInput {
				t.Errorf("code = %v, want InvalidInput", got)
			}
		})
	}
}

func TestGetWithBytes_TransportResponses(t *testing.T) {
	transportErr := errors.NewUnavailable("t", "down")
	cases := []struct {
		name string
		resp interface{}
		err  error
		code errors.ErrorCode
	}{
		{"transport error", nil, transportErr, errors.CodeUnavailable},
		{"nil response", nil, nil, errors.CodeNotFound},
		{"plain Secret refused", &Secret{Data: map[string]interface{}{"k": "v"}}, nil, errors.CodeInternal},
		{"empty data", &service.GetSecretResponse{}, nil, errors.CodeNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := NewClient(&mockTransport{
				requestFn: func(context.Context, string, string, interface{}) (interface{}, error) {
					return tc.resp, tc.err
				},
			})
			called := false
			err := c.GetWithBytes(context.Background(), "p", "k", func([]byte) error {
				called = true
				return nil
			})
			if got := errorCode(t, err); got != tc.code {
				t.Errorf("code = %v, want %v", got, tc.code)
			}
			if called {
				t.Error("fn must not run")
			}
		})
	}
}
