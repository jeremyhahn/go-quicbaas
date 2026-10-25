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

// transport_ceremony_initialize_test.go pins the arguments CeremonyInitialize
// sends to the qrdb_ceremony_initialize tool and the credentials it decodes
// from the reply.

package mcp

import (
	"context"
	"encoding/base64"
	"reflect"
	"testing"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

func TestCeremonyInitialize_SendsEveryOfficerFieldAndDecodesCredentials(t *testing.T) {
	encryptionKey := []byte{0x01, 0x02, 0xfe}
	var got map[string]interface{}
	tr, _ := newMultiToolMCPServerTLS(t, []string{"qrdb_ceremony_initialize"},
		func(_ *testing.T, _ string, args map[string]interface{}) map[string]interface{} {
			got = args
			return map[string]interface{}{
				"state":                   "READY",
				"holders":                 []interface{}{map[string]interface{}{"username": "carol", "role": "custodian", "cert_pem": "carol-cert"}},
				"admin_cert_pem":          "admin-cert",
				"admin_key_pem":           base64.StdEncoding.EncodeToString([]byte("admin-key")),
				"admin_credential_source": "generated",
			}
		})

	resp, err := tr.CeremonyInitialize(context.Background(), &qrdbtransport.CeremonyInitializeRequest{
		Threshold: 2,
		Policy:    "custodian",
		Officers: []qrdbtransport.CeremonyOfficerRequest{{
			Username:      "carol",
			Role:          "custodian",
			EncryptionKEM: "ML-KEM-768",
			EncryptionKey: encryptionKey,
			Factors: []qrdbtransport.HolderFactorEnrolment{
				{Name: "totp", Params: map[string]any{"digits": float64(6)}},
			},
			Designated: "totp",
		}},
	})
	if err != nil {
		t.Fatalf("CeremonyInitialize: %v", err)
	}

	if got["policy"] != "custodian" {
		t.Errorf("policy = %v, want custodian", got["policy"])
	}
	officers, ok := got["officers"].([]interface{})
	if !ok || len(officers) != 1 {
		t.Fatalf("officers = %#v, want one officer", got["officers"])
	}
	officer, ok := officers[0].(map[string]interface{})
	if !ok {
		t.Fatalf("officer = %#v, want an object", officers[0])
	}
	want := map[string]interface{}{
		"username":       "carol",
		"role":           "custodian",
		"encryption_kem": "ML-KEM-768",
		"encryption_key": base64.StdEncoding.EncodeToString(encryptionKey),
		"factors": []interface{}{map[string]interface{}{
			"name": "totp", "params": map[string]interface{}{"digits": float64(6)},
		}},
		"designated": "totp",
	}
	for key, value := range want {
		if !reflect.DeepEqual(officer[key], value) {
			t.Errorf("officer[%q] = %#v, want %#v", key, officer[key], value)
		}
	}

	wantHolders := []qrdbtransport.CeremonyHolderCert{{Username: "carol", Role: "custodian", CertPEM: "carol-cert"}}
	if !reflect.DeepEqual(resp.Holders, wantHolders) {
		t.Errorf("Holders = %#v, want %#v", resp.Holders, wantHolders)
	}
	if resp.AdminCertPEM != "admin-cert" {
		t.Errorf("AdminCertPEM = %q, want admin-cert", resp.AdminCertPEM)
	}
	if string(resp.AdminKeyPEM) != "admin-key" {
		t.Errorf("AdminKeyPEM = %q, want admin-key", resp.AdminKeyPEM)
	}
	if resp.AdminCredentialSource != "generated" {
		t.Errorf("AdminCredentialSource = %q, want generated", resp.AdminCredentialSource)
	}
}

func TestCeremonyInitialize_UnencodableOfficerIsInvalidInput(t *testing.T) {
	tr, called := newMultiToolMCPServerTLS(t, []string{"qrdb_ceremony_initialize"},
		func(*testing.T, string, map[string]interface{}) map[string]interface{} {
			return map[string]interface{}{"state": "READY"}
		})

	_, err := tr.CeremonyInitialize(context.Background(), &qrdbtransport.CeremonyInitializeRequest{
		Threshold: 1,
		Officers: []qrdbtransport.CeremonyOfficerRequest{{
			Username: "carol",
			Factors: []qrdbtransport.HolderFactorEnrolment{
				{Name: "totp", Params: map[string]any{"bad": make(chan int)}},
			},
		}},
	})
	if !errors.IsInvalidInput(err) {
		t.Fatalf("err = %v, want an InvalidInput error", err)
	}
	if len(*called) != 0 {
		t.Fatalf("nothing must be sent, server saw %v", *called)
	}
}
