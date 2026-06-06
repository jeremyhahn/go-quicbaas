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
// This file contains the SetKeyRotationPolicy / GetKeyRotationPolicy /
// DeleteKeyRotationPolicy extension that is additive to the generated
// kms.pb.go / kms_grpc.pb.go files.
//
// When the protobuf toolchain is available, regenerate kms.pb.go from kms.proto
// and remove this file; it is a temporary extension to avoid a full codegen run.
package proto

// RotationPolicyProto represents an automatic rotation policy in the gRPC layer.
type RotationPolicyProto struct {
	// enabled indicates if automatic rotation is active.
	Enabled bool `protobuf:"varint,1,opt,name=enabled,proto3" json:"enabled,omitempty"`
	// interval_seconds is the rotation frequency in seconds.
	IntervalSeconds int64 `protobuf:"varint,2,opt,name=interval_seconds,json=intervalSeconds,proto3" json:"interval_seconds,omitempty"`
	// next_rotation_unix is the next rotation Unix timestamp (seconds).
	NextRotationUnix int64 `protobuf:"varint,3,opt,name=next_rotation_unix,json=nextRotationUnix,proto3" json:"next_rotation_unix,omitempty"`
	// retain_versions is how many old key versions to retain.
	RetainVersions int32 `protobuf:"varint,4,opt,name=retain_versions,json=retainVersions,proto3" json:"retain_versions,omitempty"`
	// auto_rotate enables automatic rotation without manual intervention.
	AutoRotate bool `protobuf:"varint,5,opt,name=auto_rotate,json=autoRotate,proto3" json:"auto_rotate,omitempty"`
}

// GetEnabled returns whether automatic rotation is enabled.
func (x *RotationPolicyProto) GetEnabled() bool {
	if x != nil {
		return x.Enabled
	}
	return false
}

// GetIntervalSeconds returns the rotation interval in seconds.
func (x *RotationPolicyProto) GetIntervalSeconds() int64 {
	if x != nil {
		return x.IntervalSeconds
	}
	return 0
}

// GetNextRotationUnix returns the next rotation Unix timestamp.
func (x *RotationPolicyProto) GetNextRotationUnix() int64 {
	if x != nil {
		return x.NextRotationUnix
	}
	return 0
}

// GetRetainVersions returns the number of old versions to retain.
func (x *RotationPolicyProto) GetRetainVersions() int32 {
	if x != nil {
		return x.RetainVersions
	}
	return 0
}

// GetAutoRotate returns whether automatic rotation is enabled.
func (x *RotationPolicyProto) GetAutoRotate() bool {
	if x != nil {
		return x.AutoRotate
	}
	return false
}

// Reset implements proto.Message.
func (x *RotationPolicyProto) Reset() { *x = RotationPolicyProto{} }

// String implements proto.Message.
func (x *RotationPolicyProto) String() string { return "" }

// ProtoMessage implements proto.Message.
func (*RotationPolicyProto) ProtoMessage() {}

// =============================================================================
// SetKeyRotationPolicy
// =============================================================================

// SetKeyRotationPolicyRequest sets the automatic rotation policy for a key.
type SetKeyRotationPolicyRequest struct {
	// key_id is the key identifier.
	KeyId string `protobuf:"bytes,1,opt,name=key_id,json=keyId,proto3" json:"key_id,omitempty"`
	// policy is the rotation policy to apply.
	Policy *RotationPolicyProto `protobuf:"bytes,2,opt,name=policy,proto3" json:"policy,omitempty"`
}

// GetKeyId returns the key identifier.
func (x *SetKeyRotationPolicyRequest) GetKeyId() string {
	if x != nil {
		return x.KeyId
	}
	return ""
}

// GetPolicy returns the rotation policy.
func (x *SetKeyRotationPolicyRequest) GetPolicy() *RotationPolicyProto {
	if x != nil {
		return x.Policy
	}
	return nil
}

// Reset implements proto.Message.
func (x *SetKeyRotationPolicyRequest) Reset() { *x = SetKeyRotationPolicyRequest{} }

// String implements proto.Message.
func (x *SetKeyRotationPolicyRequest) String() string { return x.KeyId }

// ProtoMessage implements proto.Message.
func (*SetKeyRotationPolicyRequest) ProtoMessage() {}

// SetKeyRotationPolicyResponse confirms the rotation policy was applied.
type SetKeyRotationPolicyResponse struct {
	// key_id is the key identifier.
	KeyId string `protobuf:"bytes,1,opt,name=key_id,json=keyId,proto3" json:"key_id,omitempty"`
	// policy is the stored rotation policy (with computed next_rotation_unix).
	Policy *RotationPolicyProto `protobuf:"bytes,2,opt,name=policy,proto3" json:"policy,omitempty"`
	// timestamp is when the policy was applied (Unix seconds).
	Timestamp int64 `protobuf:"varint,3,opt,name=timestamp,proto3" json:"timestamp,omitempty"`
}

// GetKeyId returns the key identifier.
func (x *SetKeyRotationPolicyResponse) GetKeyId() string {
	if x != nil {
		return x.KeyId
	}
	return ""
}

// GetPolicy returns the stored rotation policy.
func (x *SetKeyRotationPolicyResponse) GetPolicy() *RotationPolicyProto {
	if x != nil {
		return x.Policy
	}
	return nil
}

// GetTimestamp returns the application timestamp (Unix seconds).
func (x *SetKeyRotationPolicyResponse) GetTimestamp() int64 {
	if x != nil {
		return x.Timestamp
	}
	return 0
}

// Reset implements proto.Message.
func (x *SetKeyRotationPolicyResponse) Reset() { *x = SetKeyRotationPolicyResponse{} }

// String implements proto.Message.
func (x *SetKeyRotationPolicyResponse) String() string { return x.KeyId }

// ProtoMessage implements proto.Message.
func (*SetKeyRotationPolicyResponse) ProtoMessage() {}

// =============================================================================
// GetKeyRotationPolicy
// =============================================================================

// GetKeyRotationPolicyRequest retrieves the rotation policy for a key.
type GetKeyRotationPolicyRequest struct {
	// key_id is the key identifier.
	KeyId string `protobuf:"bytes,1,opt,name=key_id,json=keyId,proto3" json:"key_id,omitempty"`
}

// GetKeyId returns the key identifier.
func (x *GetKeyRotationPolicyRequest) GetKeyId() string {
	if x != nil {
		return x.KeyId
	}
	return ""
}

// Reset implements proto.Message.
func (x *GetKeyRotationPolicyRequest) Reset() { *x = GetKeyRotationPolicyRequest{} }

// String implements proto.Message.
func (x *GetKeyRotationPolicyRequest) String() string { return x.KeyId }

// ProtoMessage implements proto.Message.
func (*GetKeyRotationPolicyRequest) ProtoMessage() {}

// GetKeyRotationPolicyResponse returns the current rotation policy.
type GetKeyRotationPolicyResponse struct {
	// key_id is the key identifier.
	KeyId string `protobuf:"bytes,1,opt,name=key_id,json=keyId,proto3" json:"key_id,omitempty"`
	// policy is the current rotation policy; absent when none is configured.
	Policy *RotationPolicyProto `protobuf:"bytes,2,opt,name=policy,proto3" json:"policy,omitempty"`
	// timestamp is when the policy was retrieved (Unix seconds).
	Timestamp int64 `protobuf:"varint,3,opt,name=timestamp,proto3" json:"timestamp,omitempty"`
}

// GetKeyId returns the key identifier.
func (x *GetKeyRotationPolicyResponse) GetKeyId() string {
	if x != nil {
		return x.KeyId
	}
	return ""
}

// GetPolicy returns the current rotation policy.
func (x *GetKeyRotationPolicyResponse) GetPolicy() *RotationPolicyProto {
	if x != nil {
		return x.Policy
	}
	return nil
}

// GetTimestamp returns the retrieval timestamp (Unix seconds).
func (x *GetKeyRotationPolicyResponse) GetTimestamp() int64 {
	if x != nil {
		return x.Timestamp
	}
	return 0
}

// Reset implements proto.Message.
func (x *GetKeyRotationPolicyResponse) Reset() { *x = GetKeyRotationPolicyResponse{} }

// String implements proto.Message.
func (x *GetKeyRotationPolicyResponse) String() string { return x.KeyId }

// ProtoMessage implements proto.Message.
func (*GetKeyRotationPolicyResponse) ProtoMessage() {}

// =============================================================================
// DeleteKeyRotationPolicy
// =============================================================================

// DeleteKeyRotationPolicyRequest removes the rotation policy for a key,
// disabling automatic rotation.
type DeleteKeyRotationPolicyRequest struct {
	// key_id is the key identifier whose rotation policy should be removed.
	KeyId string `protobuf:"bytes,1,opt,name=key_id,json=keyId,proto3" json:"key_id,omitempty"`
}

// GetKeyId returns the key identifier.
func (x *DeleteKeyRotationPolicyRequest) GetKeyId() string {
	if x != nil {
		return x.KeyId
	}
	return ""
}

// Reset implements proto.Message.
func (x *DeleteKeyRotationPolicyRequest) Reset() { *x = DeleteKeyRotationPolicyRequest{} }

// String implements proto.Message.
func (x *DeleteKeyRotationPolicyRequest) String() string { return x.KeyId }

// ProtoMessage implements proto.Message.
func (*DeleteKeyRotationPolicyRequest) ProtoMessage() {}

// DeleteKeyRotationPolicyResponse confirms the rotation policy was removed.
type DeleteKeyRotationPolicyResponse struct {
	// key_id is the key identifier.
	KeyId string `protobuf:"bytes,1,opt,name=key_id,json=keyId,proto3" json:"key_id,omitempty"`
	// timestamp is when the policy was removed (Unix seconds).
	Timestamp int64 `protobuf:"varint,2,opt,name=timestamp,proto3" json:"timestamp,omitempty"`
}

// GetKeyId returns the key identifier.
func (x *DeleteKeyRotationPolicyResponse) GetKeyId() string {
	if x != nil {
		return x.KeyId
	}
	return ""
}

// GetTimestamp returns the removal timestamp (Unix seconds).
func (x *DeleteKeyRotationPolicyResponse) GetTimestamp() int64 {
	if x != nil {
		return x.Timestamp
	}
	return 0
}

// Reset implements proto.Message.
func (x *DeleteKeyRotationPolicyResponse) Reset() { *x = DeleteKeyRotationPolicyResponse{} }

// String implements proto.Message.
func (x *DeleteKeyRotationPolicyResponse) String() string { return x.KeyId }

// ProtoMessage implements proto.Message.
func (*DeleteKeyRotationPolicyResponse) ProtoMessage() {}
