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

package proto

import (
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
)

// TestQuicBaaSService_HasNoSealRPCs pins the removal of the QuicBaaS
// service's own Seal RPCs. No server implemented them; the barrier is
// go-qrdb's, served through the Barrier service.
func TestQuicBaaSService_HasNoSealRPCs(t *testing.T) {
	removed := map[string]bool{
		"SealStatus": true,
		"SealInit":   true,
		"Unseal":     true,
		"Seal":       true,
		"SealRekey":  true,
	}
	for _, m := range QuicBaaS_ServiceDesc.Methods {
		if removed[m.MethodName] {
			t.Errorf("QuicBaaS service still has RPC %s: the barrier is go-qrdb's, served through the Barrier service", m.MethodName)
		}
	}

	for _, name := range []protoreflect.Name{
		"SealStatusRequest",
		"SealInitRequest",
		"UnsealRequest",
		"SealRequest",
		"SealRekeyRequest",
	} {
		if d := File_api_proto_quicbaas_proto.Messages().ByName(name); d != nil {
			t.Errorf("quicbaas.proto still declares message %s", name)
		}
	}

	if e := File_api_proto_quicbaas_proto.Enums().ByName("SealMode"); e != nil {
		t.Error("quicbaas.proto still declares enum SealMode")
	}
}
