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

	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// TestBarrierShareFreeResponses_DeclareNoShareMaterial pins the gRPC answers
// of init-shamir, rekey and recovery generate to go-qrdb's share-free
// responses (row #1650): go-qrdb seals each share to an enrolled officer and
// returns only the quorum and share_count, so the wire declares no field that
// could carry a share, and field 1 stays reserved.
func TestBarrierShareFreeResponses_DeclareNoShareMaterial(t *testing.T) {
	for _, msg := range []gproto.Message{
		&BarrierShamirInitializeResponse{},
		&BarrierRecoveryKeysResponse{},
	} {
		md := msg.ProtoReflect().Descriptor()
		for _, name := range []protoreflect.Name{"shares", "keys"} {
			if f := md.Fields().ByName(name); f != nil {
				t.Errorf("%s declares %q (field %d): go-qrdb returns no share material", md.FullName(), name, f.Number())
			}
		}
		if f := md.Fields().ByNumber(1); f != nil {
			t.Errorf("%s field 1 is %q, want it reserved", md.FullName(), f.Name())
		}
		if md.Fields().ByName("share_count") == nil {
			t.Errorf("%s lacks go-qrdb's share_count", md.FullName())
		}
	}
}
