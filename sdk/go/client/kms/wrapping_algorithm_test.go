// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package kms

import "testing"

func TestWrappingAlgorithm_ServiceName(t *testing.T) {
	want := map[WrappingAlgorithm]string{
		WrapAlgAESKW:         "AES-KW",
		WrapAlgAESKWP:        "AES-KWP",
		WrapAlgAESGCM:        "AES-GCM",
		WrapAlgRSAOAEPSHA256: "RSA-OAEP-SHA256",
		WrapAlgRSAOAEPSHA384: "RSA-OAEP-SHA384",
		WrapAlgRSAOAEPSHA512: "RSA-OAEP-SHA512",
		WrapAlgECDHESA256KW:  "ECDH-ES+A256KW",
	}
	for alg, name := range want {
		got, known := alg.ServiceName()
		if !known || got != name {
			t.Errorf("WrappingAlgorithm(%d).ServiceName() = %q, %v; want %q, true", alg, got, known, name)
		}
	}
}

func TestWrappingAlgorithm_ServiceName_Unknown(t *testing.T) {
	for _, alg := range []WrappingAlgorithm{-1, WrapAlgECDHESA256KW + 1, 99} {
		if got, known := alg.ServiceName(); known || got != "" {
			t.Errorf("WrappingAlgorithm(%d).ServiceName() = %q, %v; want \"\", false", alg, got, known)
		}
	}
}
