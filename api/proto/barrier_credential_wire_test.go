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

package proto

import (
	"bytes"
	"testing"

	gproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

// retypedCredentials lists every barrier credential field go-quicbaas#962
// retyped from `string` to `bytes`. BarrierShamirInitializeRequest is absent:
// it carries no secret, as go-qrdb v0.50 reserves that field on its message.
var retypedCredentials = []struct {
	msg   gproto.Message
	field protoreflect.Name
}{
	{&BarrierInitializeRequest{}, "secret"},
	{&BarrierUnsealRequest{}, "secret"},
	{&BarrierEnableEncryptionRequest{}, "passphrase"},
	{&TenantBarrierInitRequest{}, "passphrase"},
	{&TenantBarrierEnableEncryptionRequest{}, "passphrase"},
}

// stringTypedDescriptor rebuilds md's file with field typed `string`, which
// is the message as it was declared before the retype.
func stringTypedDescriptor(t *testing.T, md protoreflect.MessageDescriptor, field protoreflect.Name) protoreflect.MessageDescriptor {
	t.Helper()
	fdp := protodesc.ToFileDescriptorProto(md.ParentFile())
	retyped := false
	for _, m := range fdp.GetMessageType() {
		if m.GetName() != string(md.Name()) {
			continue
		}
		for _, f := range m.GetField() {
			if f.GetName() == string(field) {
				f.Type = descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()
				retyped = true
			}
		}
	}
	if !retyped {
		t.Fatalf("%s has no field %q", md.FullName(), field)
	}
	fd, err := protodesc.NewFile(fdp, protoregistry.GlobalFiles)
	if err != nil {
		t.Fatalf("build string-typed %s: %v", md.ParentFile().Path(), err)
	}
	return fd.Messages().ByName(md.Name())
}

// TestBarrierCredentialFrames_StringAndBytesEncodeIdentically proves the
// retype is not a wire change on gRPC, Unix or QUIC: proto3 encodes `bytes`
// and `string` as the same length-delimited field, so a frame from the old
// declaration and one from the new are byte-for-byte equal, and each decodes
// into the other.
func TestBarrierCredentialFrames_StringAndBytesEncodeIdentically(t *testing.T) {
	const credential = "correct horse battery staple ✓"
	marshal := gproto.MarshalOptions{Deterministic: true}

	for _, tc := range retypedCredentials {
		md := tc.msg.ProtoReflect().Descriptor()
		t.Run(string(md.Name()), func(t *testing.T) {
			legacyMD := stringTypedDescriptor(t, md, tc.field)

			current := tc.msg.ProtoReflect().New()
			current.Set(md.Fields().ByName(tc.field), protoreflect.ValueOfBytes([]byte(credential)))
			legacy := dynamicpb.NewMessage(legacyMD)
			legacy.Set(legacyMD.Fields().ByName(tc.field), protoreflect.ValueOfString(credential))

			currentFrame, err := marshal.Marshal(current.Interface())
			if err != nil {
				t.Fatalf("marshal bytes-typed message: %v", err)
			}
			legacyFrame, err := marshal.Marshal(legacy)
			if err != nil {
				t.Fatalf("marshal string-typed message: %v", err)
			}
			if !bytes.Equal(currentFrame, legacyFrame) {
				t.Fatalf("frames differ:\n bytes:  %x\n string: %x", currentFrame, legacyFrame)
			}

			decoded := tc.msg.ProtoReflect().New()
			if err := gproto.Unmarshal(legacyFrame, decoded.Interface()); err != nil {
				t.Fatalf("decode string-typed frame as bytes: %v", err)
			}
			if got := decoded.Get(md.Fields().ByName(tc.field)).Bytes(); string(got) != credential {
				t.Fatalf("bytes-typed decode = %q, want %q", got, credential)
			}
			legacyDecoded := dynamicpb.NewMessage(legacyMD)
			if err := gproto.Unmarshal(currentFrame, legacyDecoded); err != nil {
				t.Fatalf("decode bytes-typed frame as string: %v", err)
			}
			if got := legacyDecoded.Get(legacyMD.Fields().ByName(tc.field)).String(); got != credential {
				t.Fatalf("string-typed decode = %q, want %q", got, credential)
			}
		})
	}
}

// TestBarrierCredentialFrames_NonUTF8CredentialRoundTrips is the reason for
// the retype: a credential is arbitrary bytes, and a proto3 `string` field
// refuses to encode bytes that are not valid UTF-8.
func TestBarrierCredentialFrames_NonUTF8CredentialRoundTrips(t *testing.T) {
	credential := []byte{0x00, 0xff, 0xfe, 0x80, 0x7f}

	for _, tc := range retypedCredentials {
		md := tc.msg.ProtoReflect().Descriptor()
		t.Run(string(md.Name()), func(t *testing.T) {
			legacyMD := stringTypedDescriptor(t, md, tc.field)
			legacy := dynamicpb.NewMessage(legacyMD)
			legacy.Set(legacyMD.Fields().ByName(tc.field), protoreflect.ValueOfString(string(credential)))
			if _, err := gproto.Marshal(legacy); err == nil {
				t.Fatal("the string-typed field encoded invalid UTF-8; expected a refusal")
			}

			current := tc.msg.ProtoReflect().New()
			current.Set(md.Fields().ByName(tc.field), protoreflect.ValueOfBytes(credential))
			frame, err := gproto.Marshal(current.Interface())
			if err != nil {
				t.Fatalf("marshal bytes-typed message: %v", err)
			}
			decoded := tc.msg.ProtoReflect().New()
			if err := gproto.Unmarshal(frame, decoded.Interface()); err != nil {
				t.Fatalf("unmarshal bytes-typed message: %v", err)
			}
			if got := decoded.Get(md.Fields().ByName(tc.field)).Bytes(); !bytes.Equal(got, credential) {
				t.Fatalf("round trip = %x, want %x", got, credential)
			}
		})
	}
}
