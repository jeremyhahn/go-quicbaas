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

// Package proto provides protocol buffer message types for the QuicBaaS KMS API.
// This file contains the DeleteKeyPolicy extension that is additive to the
// generated kms.pb.go / kms_grpc.pb.go files.
//
// When the protobuf toolchain is available, regenerate kms.pb.go from kms.proto
// and remove this file; it is a temporary extension to avoid a full codegen run.
package proto

// DeleteKeyPolicyRequest removes the access control policy for a key.
type DeleteKeyPolicyRequest struct {
	// key_id is the key identifier whose policy should be removed.
	KeyId string `protobuf:"bytes,1,opt,name=key_id,json=keyId,proto3" json:"key_id,omitempty"`
}

// GetKeyId returns the key identifier.
func (x *DeleteKeyPolicyRequest) GetKeyId() string {
	if x != nil {
		return x.KeyId
	}
	return ""
}

// Reset implements proto.Message.
func (x *DeleteKeyPolicyRequest) Reset() { *x = DeleteKeyPolicyRequest{} }

// String implements proto.Message.
func (x *DeleteKeyPolicyRequest) String() string { return x.KeyId }

// ProtoMessage implements proto.Message.
func (*DeleteKeyPolicyRequest) ProtoMessage() {}

// DeleteKeyPolicyResponse confirms that the policy was removed.
type DeleteKeyPolicyResponse struct {
	// key_id is the key identifier.
	KeyId string `protobuf:"bytes,1,opt,name=key_id,json=keyId,proto3" json:"key_id,omitempty"`
	// timestamp is when the policy was removed (Unix seconds).
	Timestamp int64 `protobuf:"varint,2,opt,name=timestamp,proto3" json:"timestamp,omitempty"`
}

// GetKeyId returns the key identifier.
func (x *DeleteKeyPolicyResponse) GetKeyId() string {
	if x != nil {
		return x.KeyId
	}
	return ""
}

// GetTimestamp returns the deletion timestamp (Unix seconds).
func (x *DeleteKeyPolicyResponse) GetTimestamp() int64 {
	if x != nil {
		return x.Timestamp
	}
	return 0
}

// Reset implements proto.Message.
func (x *DeleteKeyPolicyResponse) Reset() { *x = DeleteKeyPolicyResponse{} }

// String implements proto.Message.
func (x *DeleteKeyPolicyResponse) String() string { return x.KeyId }

// ProtoMessage implements proto.Message.
func (*DeleteKeyPolicyResponse) ProtoMessage() {}
