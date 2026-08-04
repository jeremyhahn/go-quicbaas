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

package quic

import (
	"testing"

	kmsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/kms"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// TestUpdateKeyStatus_OpcodeResolves pins the method-name alias: the SDK
// client sends "UpdateKeyStatus", which must resolve to the same opcode as
// the server-registered "KeyStatus" verb.
func TestUpdateKeyStatus_OpcodeResolves(t *testing.T) {
	op, ok := MethodToOpcode("quicbaas.KMS", "UpdateKeyStatus")
	if !ok {
		t.Fatal("UpdateKeyStatus does not resolve to an opcode")
	}
	if legacy, _ := MethodToOpcode("quicbaas.KMS", "KeyStatus"); legacy != op {
		t.Errorf("UpdateKeyStatus opcode %#x != KeyStatus opcode %#x", op, legacy)
	}
}

// TestTransformKMSUpdateKeyStatus_ConvertsEnum verifies the int-enum SDK
// request becomes the service string-status wire shape.
func TestTransformKMSUpdateKeyStatus_ConvertsEnum(t *testing.T) {
	out, err := transformKMSUpdateKeyStatus(&kmsclient.UpdateKeyStatusRequest{
		KeyID:   "k1",
		Version: 3,
		Status:  kmsclient.KeyStatusDisabled,
	})
	if err != nil {
		t.Fatalf("transform: %v", err)
	}
	svcReq, ok := out.(*service.UpdateKeyStatusRequest)
	if !ok {
		t.Fatalf("expected *service.UpdateKeyStatusRequest, got %T", out)
	}
	if svcReq.KeyID != "k1" || svcReq.Version != 3 || svcReq.Status != service.KeyStatusDisabled {
		t.Errorf("transform mismatch: %+v", svcReq)
	}
}

// TestTransformKMSUpdateKeyStatus_InvalidType pins the error path.
func TestTransformKMSUpdateKeyStatus_InvalidType(t *testing.T) {
	if _, err := transformKMSUpdateKeyStatus("bogus"); err == nil {
		t.Fatal("expected error for invalid request type")
	}
}
