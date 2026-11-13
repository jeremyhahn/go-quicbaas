// Copyright 2026 Jeremy Hahn
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package security

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/secret"
)

// testSignKey seals key into a resolved SecretValue, the shape the config
// processor leaves in AuditConfig.SignKey. It panics on a seal failure so it
// can sit in struct literals and test tables.
func testSignKey(key string) *secret.SecretValue {
	v, err := secret.NewSecretValue("literal://test-audit-sign-key", []byte(key), secret.StrategyLiteral)
	if err != nil {
		panic(fmt.Sprintf("seal test sign key: %v", err))
	}
	return v
}

// TestAuditSignKey_NeverPlainMemory pins issue #23: neither the config nor
// the logger carries the HMAC key as a Go string or byte slice. Before the
// fix AuditConfig.SignKey was a string and AuditLogger.signKey a []byte.
func TestAuditSignKey_NeverPlainMemory(t *testing.T) {
	want := reflect.TypeOf((*secret.SecretValue)(nil))

	cfgField, ok := reflect.TypeOf(AuditConfig{}).FieldByName("SignKey")
	if !ok {
		t.Fatal("AuditConfig.SignKey missing")
	}
	if cfgField.Type != want {
		t.Errorf("AuditConfig.SignKey is %v, want %v", cfgField.Type, want)
	}

	alType := reflect.TypeOf(AuditLogger{})
	keyField, ok := alType.FieldByName("signKey")
	if !ok {
		t.Fatal("AuditLogger.signKey missing")
	}
	if keyField.Type != want {
		t.Errorf("AuditLogger.signKey is %v, want %v", keyField.Type, want)
	}
	for i := 0; i < alType.NumField(); i++ {
		f := alType.Field(i)
		if !strings.Contains(strings.ToLower(f.Name), "key") {
			continue
		}
		if f.Type.Kind() == reflect.String || f.Type == reflect.TypeOf([]byte(nil)) {
			t.Errorf("AuditLogger.%s holds key material as plain %v", f.Name, f.Type)
		}
	}
}

// TestAuditConfig_SignKeyJSONKeepsReference: sign_key decodes as a reference,
// and re-encoding the config never emits a resolved key.
func TestAuditConfig_SignKeyJSONKeepsReference(t *testing.T) {
	var cfg AuditConfig
	in := `{"enabled":true,"sign_logs":true,"sign_key":"credential://audit-sign"}`
	if err := json.Unmarshal([]byte(in), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if cfg.SignKey == nil || cfg.SignKey.Raw != "credential://audit-sign" {
		t.Fatalf("SignKey = %+v, want reference credential://audit-sign", cfg.SignKey)
	}

	cfg.SignKey = testSignKey("resolved-plaintext-hmac-key")
	cfg.SignKey.Raw = "credential://audit-sign"
	out, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(out), "resolved-plaintext-hmac-key") {
		t.Fatalf("marshalled config leaks the key: %s", out)
	}
	if !strings.Contains(string(out), `"sign_key":"credential://audit-sign"`) {
		t.Fatalf("marshalled config lost the reference: %s", out)
	}
	if s := fmt.Sprintf("%v %+v", cfg, cfg.SignKey); strings.Contains(s, "resolved-plaintext-hmac-key") {
		t.Fatalf("formatting leaks the key: %s", s)
	}
}

// TestNewAuditLogger_RejectsUnresolvedSignKey: a reference the config
// processor never resolved has no key behind it, and signing with no key
// must fail closed at construction.
func TestNewAuditLogger_RejectsUnresolvedSignKey(t *testing.T) {
	_, err := NewAuditLogger(&AuditConfig{
		Enabled:  true,
		SignLogs: true,
		SignKey:  &secret.SecretValue{Raw: "credential://audit-sign"},
		File:     filepath.Join(t.TempDir(), "audit.log"),
	}, createTestLogger(t))
	var ae *AuditError
	if !errors.As(err, &ae) || ae.Code != ErrCodeAuditSignKeyRequired {
		t.Fatalf("err = %v, want %s", err, ErrCodeAuditSignKeyRequired)
	}

	_, err = NewAuditLogger(&AuditConfig{Enabled: true, SignLogs: true}, createTestLogger(t))
	if !errors.As(err, &ae) || ae.Code != ErrCodeAuditSignKeyRequired {
		t.Fatalf("nil SignKey: err = %v, want %s", err, ErrCodeAuditSignKeyRequired)
	}
}

// TestAuditLogger_OwnsSealedKeyCopy: the logger signs with its own sealed
// copy, so the caller clearing the config's value does not break signing,
// and Close destroys the logger's copy so signing then fails closed.
func TestAuditLogger_OwnsSealedKeyCopy(t *testing.T) {
	const key = "owned-copy-hmac-key-32-bytes!!!"
	cfg := &AuditConfig{
		Enabled:  true,
		SignLogs: true,
		SignKey:  testSignKey(key),
		File:     filepath.Join(t.TempDir(), "audit.log"),
	}
	al, err := NewAuditLogger(cfg, createTestLogger(t))
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	cfg.SignKey.Clear()

	event := &AuditEvent{
		Timestamp: time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC),
		Username:  "alice",
		Action:    ActionRead,
		Resource:  "doc",
		Result:    ResultSuccess,
		IPAddress: "192.0.2.1",
	}
	sig, err := al.signEvent(event)
	if err != nil {
		t.Fatalf("signEvent after caller Clear: %v", err)
	}
	h := hmac.New(sha256.New, []byte(key))
	h.Write([]byte("2026-11-13T00:00:00Z|alice|" + string(ActionRead) + "|doc|" + string(ResultSuccess) + "|192.0.2.1"))
	if want := hex.EncodeToString(h.Sum(nil)); sig != want {
		t.Fatalf("signature = %s, want %s", sig, want)
	}
	event.Signature = sig
	if !al.VerifySignature(event) {
		t.Fatal("VerifySignature rejected a valid signature")
	}

	if err := al.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := al.signEvent(event); err == nil {
		t.Fatal("signEvent after Close succeeded; the key must be destroyed")
	}
	if al.VerifySignature(event) {
		t.Fatal("VerifySignature after Close accepted an event")
	}
}
