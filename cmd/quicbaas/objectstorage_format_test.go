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

// Package main provides tests for object storage format functions to improve coverage.
package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
)

// =============================================================================
// Object Storage Format Function Tests
// =============================================================================

func TestFormatObjectPutResponse(t *testing.T) {
	tests := []struct {
		name         string
		response     *pb.PutObjectResponse
		outputFormat string
		expectError  bool
	}{
		{
			name: "table format with replication",
			response: &pb.PutObjectResponse{
				Key:          "mybucket/mykey",
				Size:         1024,
				Version:      1,
				Checksum:     []byte{0x01, 0x02, 0x03, 0x04},
				CreatedAt:    timestamppb.Now(),
				ReplicatedTo: []string{"node-1", "node-2"},
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "json format",
			response: &pb.PutObjectResponse{
				Key:       "mybucket/mykey",
				Size:      1024,
				Version:   1,
				Checksum:  []byte{0x01, 0x02, 0x03, 0x04},
				CreatedAt: timestamppb.Now(),
			},
			outputFormat: "json",
			expectError:  false,
		},
		{
			name: "yaml format",
			response: &pb.PutObjectResponse{
				Key:      "mybucket/mykey",
				Size:     1024,
				Version:  1,
				Checksum: []byte{0x01, 0x02, 0x03, 0x04},
			},
			outputFormat: "yaml",
			expectError:  false,
		},
		{
			name: "table format no replication",
			response: &pb.PutObjectResponse{
				Key:          "mybucket/mykey",
				Size:         1024,
				Version:      1,
				Checksum:     []byte{0x01, 0x02, 0x03, 0x04},
				ReplicatedTo: []string{},
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "nil created at",
			response: &pb.PutObjectResponse{
				Key:       "mybucket/mykey",
				Size:      1024,
				Version:   1,
				Checksum:  []byte{0x01, 0x02, 0x03, 0x04},
				CreatedAt: nil,
			},
			outputFormat: "table",
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objectOutputFormat = tt.outputFormat
			err := formatObjectPutResponse(tt.response)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFormatObjectResponsesInvalidFormat(t *testing.T) {
	tests := []struct {
		name   string
		format func() error
	}{
		{"put", func() error { return formatObjectPutResponse(&pb.PutObjectResponse{Key: "mybucket/mykey"}) }},
		{"get", func() error {
			return formatObjectGetResponse(&pb.GetObjectResponse{Key: "mybucket/mykey", Found: true})
		}},
		{"delete", func() error { return formatObjectDeleteResponse(&pb.DeleteObjectResponse{Key: "mybucket/mykey"}) }},
		{"head", func() error { return formatObjectHeadResponse(&pb.HeadObjectResponse{Key: "mybucket/mykey"}) }},
		{"list", func() error { return formatObjectListResponse(&pb.ListObjectsResponse{}) }},
		{"list-by-tag", func() error { return formatObjectListByTagResponse(&pb.ListByTagResponse{}) }},
		{"list-by-tenant", func() error { return formatObjectListByTenantResponse(&pb.ListByTenantResponse{}) }},
		{"copy", func() error { return formatObjectCopyResponse(&pb.CopyObjectResponse{Key: "mybucket/copy-key"}) }},
		{"archive", func() error { return formatObjectArchiveResponse(&pb.ArchiveObjectResponse{Key: "mybucket/mykey"}) }},
		{"restore", func() error { return formatObjectRestoreResponse(&pb.RestoreObjectResponse{Key: "mybucket/mykey"}) }},
		{"verify", func() error { return formatObjectVerifyResponse(&pb.VerifyObjectResponse{Key: "mybucket/mykey"}) }},
		{"replicate", func() error { return formatObjectReplicateResponse(&pb.ReplicateObjectResponse{Key: "mybucket/mykey"}) }},
		{"migrate", func() error { return formatObjectMigrateResponse(&pb.MigrateObjectResponse{Key: "mybucket/mykey"}) }},
		{"stats", func() error { return formatObjectStatsResponse(&pb.ListBackendsResponse{}) }},
		{"lifecycle", func() error {
			return formatObjectLifecycleResponse("mybucket", &pb.GetObjectPolicyResponse{Key: "mybucket/mykey", Policy: &pb.ObjectPolicy{}})
		}},
	}

	saved := objectOutputFormat
	t.Cleanup(func() { objectOutputFormat = saved })
	objectOutputFormat = "invalid-format"

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Error(t, tt.format())
		})
	}
}

func TestFormatObjectGetResponse(t *testing.T) {
	tests := []struct {
		name         string
		response     *pb.GetObjectResponse
		outputFormat string
		includeMeta  bool
		expectError  bool
	}{
		{
			name: "json format with metadata",
			response: &pb.GetObjectResponse{
				Key:         "mybucket/mykey",
				Value:       []byte("hello world"),
				Found:       true,
				Size:        11,
				Version:     1,
				ContentType: "text/plain",
				Checksum:    []byte{0x01, 0x02, 0x03, 0x04},
				Tags:        map[string]string{"env": "prod"},
				Metadata: &pb.ObjectMetadata{
					TenantId: "tenant-1",
					Owner:    "user-1",
					Status:   pb.ObjectStatus_OBJECT_STATUS_ACTIVE,
				},
			},
			outputFormat: "json",
			includeMeta:  true,
			expectError:  false,
		},
		{
			name: "yaml format",
			response: &pb.GetObjectResponse{
				Key:         "mybucket/mykey",
				Value:       []byte("hello world"),
				Found:       true,
				Size:        11,
				Version:     1,
				ContentType: "text/plain",
				Tags:        map[string]string{"env": "prod"},
			},
			outputFormat: "yaml",
			includeMeta:  false,
			expectError:  false,
		},
		{
			name: "raw format",
			response: &pb.GetObjectResponse{
				Key:   "mybucket/mykey",
				Value: []byte("hello world"),
				Found: true,
			},
			outputFormat: "raw",
			includeMeta:  false,
			expectError:  false,
		},
		{
			name: "table format (defaults to raw)",
			response: &pb.GetObjectResponse{
				Key:   "mybucket/mykey",
				Value: []byte("hello world"),
				Found: true,
			},
			outputFormat: "table",
			includeMeta:  false,
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objectOutputFormat = tt.outputFormat
			objectIncludeMeta = tt.includeMeta
			err := formatObjectGetResponse(tt.response)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFormatObjectDeleteResponse(t *testing.T) {
	tests := []struct {
		name         string
		response     *pb.DeleteObjectResponse
		outputFormat string
		expectError  bool
	}{
		{
			name: "deleted table format permanent",
			response: &pb.DeleteObjectResponse{
				Key:       "mybucket/mykey",
				Deleted:   true,
				Permanent: true,
				DeletedAt: timestamppb.Now(),
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "deleted table format soft delete",
			response: &pb.DeleteObjectResponse{
				Key:       "mybucket/mykey",
				Deleted:   true,
				Permanent: false,
				DeletedAt: timestamppb.Now(),
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "not found table format",
			response: &pb.DeleteObjectResponse{
				Key:     "mybucket/mykey",
				Deleted: false,
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "json format",
			response: &pb.DeleteObjectResponse{
				Key:     "mybucket/mykey",
				Deleted: true,
			},
			outputFormat: "json",
			expectError:  false,
		},
		{
			name: "yaml format",
			response: &pb.DeleteObjectResponse{
				Key:     "mybucket/mykey",
				Deleted: true,
			},
			outputFormat: "yaml",
			expectError:  false,
		},
		{
			name: "nil deleted at",
			response: &pb.DeleteObjectResponse{
				Key:       "mybucket/mykey",
				Deleted:   true,
				Permanent: true,
				DeletedAt: nil,
			},
			outputFormat: "table",
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objectOutputFormat = tt.outputFormat
			err := formatObjectDeleteResponse(tt.response)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFormatObjectHeadResponse(t *testing.T) {
	tests := []struct {
		name         string
		response     *pb.HeadObjectResponse
		outputFormat string
		expectError  bool
	}{
		{
			name: "exists table format",
			response: &pb.HeadObjectResponse{
				Key:         "mybucket/mykey",
				Exists:      true,
				Size:        1024,
				ContentType: "application/json",
				Version:     1,
				Checksum:    []byte{0x01, 0x02, 0x03, 0x04},
				Status:      pb.ObjectStatus_OBJECT_STATUS_ACTIVE,
				Tier:        pb.StorageTier_STORAGE_TIER_HOT,
				ModifiedAt:  timestamppb.Now(),
				Tags:        map[string]string{"env": "prod"},
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "not exists table format",
			response: &pb.HeadObjectResponse{
				Key:    "mybucket/mykey",
				Exists: false,
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "json format",
			response: &pb.HeadObjectResponse{
				Key:    "mybucket/mykey",
				Exists: true,
				Size:   1024,
			},
			outputFormat: "json",
			expectError:  false,
		},
		{
			name: "yaml format",
			response: &pb.HeadObjectResponse{
				Key:    "mybucket/mykey",
				Exists: true,
				Size:   1024,
			},
			outputFormat: "yaml",
			expectError:  false,
		},
		{
			name: "no tags",
			response: &pb.HeadObjectResponse{
				Key:        "mybucket/mykey",
				Exists:     true,
				Size:       1024,
				Tags:       map[string]string{},
				ModifiedAt: nil,
			},
			outputFormat: "table",
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objectOutputFormat = tt.outputFormat
			err := formatObjectHeadResponse(tt.response)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFormatObjectListResponse(t *testing.T) {
	tests := []struct {
		name         string
		response     *pb.ListObjectsResponse
		outputFormat string
		expectError  bool
	}{
		{
			name: "with objects table format",
			response: &pb.ListObjectsResponse{
				Objects: []*pb.ObjectInfo{
					{Key: "key1", Size: 1024, Version: 1, ModifiedAt: timestamppb.Now()},
					{Key: "key2", Size: 2048, Version: 2, ModifiedAt: timestamppb.Now()},
				},
				Total:      2,
				NextCursor: "",
				HasMore:    false,
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "with pagination table format",
			response: &pb.ListObjectsResponse{
				Objects: []*pb.ObjectInfo{
					{Key: "key1", Size: 1024, Version: 1, ModifiedAt: timestamppb.Now()},
				},
				Total:      100,
				NextCursor: "next-cursor-123",
				HasMore:    true,
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "json format",
			response: &pb.ListObjectsResponse{
				Objects: []*pb.ObjectInfo{
					{Key: "key1", Size: 1024, Version: 1},
				},
				Total: 1,
			},
			outputFormat: "json",
			expectError:  false,
		},
		{
			name: "yaml format",
			response: &pb.ListObjectsResponse{
				Objects: []*pb.ObjectInfo{
					{Key: "key1", Size: 1024, Version: 1},
				},
				Total: 1,
			},
			outputFormat: "yaml",
			expectError:  false,
		},
		{
			name: "nil modified at",
			response: &pb.ListObjectsResponse{
				Objects: []*pb.ObjectInfo{
					{Key: "key1", Size: 1024, Version: 1, ModifiedAt: nil},
				},
				Total: 1,
			},
			outputFormat: "table",
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objectOutputFormat = tt.outputFormat
			err := formatObjectListResponse(tt.response)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFormatObjectListByTagResponse(t *testing.T) {
	tests := []struct {
		name         string
		response     *pb.ListByTagResponse
		outputFormat string
		expectError  bool
	}{
		{
			name: "with keys table format",
			response: &pb.ListByTagResponse{
				Keys:       []string{"key1", "key2", "key3"},
				Total:      3,
				NextCursor: "",
				HasMore:    false,
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "with pagination table format",
			response: &pb.ListByTagResponse{
				Keys:       []string{"key1"},
				Total:      100,
				NextCursor: "next-cursor-123",
				HasMore:    true,
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "json format",
			response: &pb.ListByTagResponse{
				Keys:  []string{"key1"},
				Total: 1,
			},
			outputFormat: "json",
			expectError:  false,
		},
		{
			name: "yaml format",
			response: &pb.ListByTagResponse{
				Keys:  []string{"key1"},
				Total: 1,
			},
			outputFormat: "yaml",
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objectOutputFormat = tt.outputFormat
			err := formatObjectListByTagResponse(tt.response)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFormatObjectListByTenantResponse(t *testing.T) {
	tests := []struct {
		name         string
		response     *pb.ListByTenantResponse
		outputFormat string
		expectError  bool
	}{
		{
			name: "with objects table format",
			response: &pb.ListByTenantResponse{
				Objects: []*pb.ObjectInfo{
					{Key: "key1", Size: 1024, Version: 1},
					{Key: "key2", Size: 2048, Version: 2},
				},
				Total:      2,
				NextCursor: "",
				HasMore:    false,
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "with keys only table format",
			response: &pb.ListByTenantResponse{
				Keys:       []string{"key1", "key2"},
				Objects:    []*pb.ObjectInfo{},
				Total:      2,
				NextCursor: "",
				HasMore:    false,
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "with pagination table format",
			response: &pb.ListByTenantResponse{
				Objects: []*pb.ObjectInfo{
					{Key: "key1", Size: 1024, Version: 1},
				},
				Total:      100,
				NextCursor: "next-cursor-123",
				HasMore:    true,
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "json format",
			response: &pb.ListByTenantResponse{
				Objects: []*pb.ObjectInfo{
					{Key: "key1", Size: 1024, Version: 1},
				},
				Total: 1,
			},
			outputFormat: "json",
			expectError:  false,
		},
		{
			name: "yaml format",
			response: &pb.ListByTenantResponse{
				Objects: []*pb.ObjectInfo{
					{Key: "key1", Size: 1024, Version: 1},
				},
				Total: 1,
			},
			outputFormat: "yaml",
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objectOutputFormat = tt.outputFormat
			err := formatObjectListByTenantResponse(tt.response)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFormatObjectCopyResponse(t *testing.T) {
	tests := []struct {
		name         string
		response     *pb.CopyObjectResponse
		outputFormat string
		expectError  bool
	}{
		{
			name: "table format",
			response: &pb.CopyObjectResponse{
				Key:       "mybucket/copy-key",
				Version:   1,
				Size:      1024,
				CreatedAt: timestamppb.Now(),
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "json format",
			response: &pb.CopyObjectResponse{
				Key:     "mybucket/copy-key",
				Version: 1,
				Size:    1024,
			},
			outputFormat: "json",
			expectError:  false,
		},
		{
			name: "nil created at",
			response: &pb.CopyObjectResponse{
				Key:       "mybucket/copy-key",
				Version:   1,
				Size:      1024,
				CreatedAt: nil,
			},
			outputFormat: "table",
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objectOutputFormat = tt.outputFormat
			err := formatObjectCopyResponse(tt.response)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFormatObjectArchiveResponse(t *testing.T) {
	tests := []struct {
		name         string
		response     *pb.ArchiveObjectResponse
		outputFormat string
		expectError  bool
	}{
		{
			name: "success table format",
			response: &pb.ArchiveObjectResponse{
				Key:          "mybucket/mykey",
				Success:      true,
				ArchiveStore: "cold-storage",
				ArchivedAt:   timestamppb.Now(),
				ArchivedSize: 512,
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "failure table format",
			response: &pb.ArchiveObjectResponse{
				Key:     "mybucket/mykey",
				Success: false,
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "json format",
			response: &pb.ArchiveObjectResponse{
				Key:          "mybucket/mykey",
				Success:      true,
				ArchiveStore: "cold-storage",
			},
			outputFormat: "json",
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objectOutputFormat = tt.outputFormat
			err := formatObjectArchiveResponse(tt.response)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFormatObjectRestoreResponse(t *testing.T) {
	tests := []struct {
		name         string
		response     *pb.RestoreObjectResponse
		outputFormat string
		expectError  bool
	}{
		{
			name: "success table format",
			response: &pb.RestoreObjectResponse{
				Key:                 "mybucket/mykey",
				Success:             true,
				RestoredTo:          "hot-storage",
				OperationId:         "op-123",
				EstimatedCompletion: timestamppb.Now(),
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "failure table format",
			response: &pb.RestoreObjectResponse{
				Key:     "mybucket/mykey",
				Success: false,
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "json format",
			response: &pb.RestoreObjectResponse{
				Key:         "mybucket/mykey",
				Success:     true,
				RestoredTo:  "hot-storage",
				OperationId: "op-456",
			},
			outputFormat: "json",
			expectError:  false,
		},
		{
			name: "yaml format",
			response: &pb.RestoreObjectResponse{
				Key:         "mybucket/mykey",
				Success:     true,
				RestoredTo:  "warm-storage",
				OperationId: "op-789",
			},
			outputFormat: "yaml",
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objectOutputFormat = tt.outputFormat
			err := formatObjectRestoreResponse(tt.response)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFormatObjectVerifyResponse(t *testing.T) {
	tests := []struct {
		name         string
		response     *pb.VerifyObjectResponse
		outputFormat string
		expectError  bool
	}{
		{
			name: "valid table format",
			response: &pb.VerifyObjectResponse{
				Key:      "mybucket/mykey",
				Valid:    true,
				Repaired: false,
				Locations: []*pb.LocationVerification{
					{Store: "store-1", Valid: true, Checksum: []byte{0x01, 0x02}, Size: 1024},
					{Store: "store-2", Valid: true, Checksum: []byte{0x01, 0x02}, Size: 1024},
				},
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "invalid with repair table format",
			response: &pb.VerifyObjectResponse{
				Key:      "mybucket/mykey",
				Valid:    false,
				Repaired: true,
				Locations: []*pb.LocationVerification{
					{Store: "store-1", Valid: true, Checksum: []byte{0x01, 0x02}, Size: 1024},
					{Store: "store-2", Valid: false, Error: "checksum mismatch", Checksum: []byte{0x03, 0x04}, Size: 1024},
				},
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "json format",
			response: &pb.VerifyObjectResponse{
				Key:      "mybucket/mykey",
				Valid:    true,
				Repaired: false,
			},
			outputFormat: "json",
			expectError:  false,
		},
		{
			name: "yaml format",
			response: &pb.VerifyObjectResponse{
				Key:      "mybucket/mykey",
				Valid:    true,
				Repaired: false,
				Locations: []*pb.LocationVerification{
					{Store: "store-1", Valid: true},
				},
			},
			outputFormat: "yaml",
			expectError:  false,
		},
		{
			name: "no locations",
			response: &pb.VerifyObjectResponse{
				Key:       "mybucket/mykey",
				Valid:     true,
				Repaired:  false,
				Locations: []*pb.LocationVerification{},
			},
			outputFormat: "table",
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objectOutputFormat = tt.outputFormat
			err := formatObjectVerifyResponse(tt.response)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFormatObjectReplicateResponse(t *testing.T) {
	tests := []struct {
		name         string
		response     *pb.ReplicateObjectResponse
		outputFormat string
		expectError  bool
	}{
		{
			name: "success table format",
			response: &pb.ReplicateObjectResponse{
				Key:          "mybucket/mykey",
				Success:      true,
				ReplicatedTo: []string{"backend-1", "backend-2"},
				FailedStores: []string{},
				OperationId:  "op-123",
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "partial failure table format",
			response: &pb.ReplicateObjectResponse{
				Key:          "mybucket/mykey",
				Success:      false,
				ReplicatedTo: []string{"backend-1"},
				FailedStores: []string{"backend-2"},
				OperationId:  "op-456",
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "json format",
			response: &pb.ReplicateObjectResponse{
				Key:          "mybucket/mykey",
				Success:      true,
				ReplicatedTo: []string{"backend-1"},
			},
			outputFormat: "json",
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objectOutputFormat = tt.outputFormat
			err := formatObjectReplicateResponse(tt.response)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFormatObjectMigrateResponse(t *testing.T) {
	tests := []struct {
		name         string
		response     *pb.MigrateObjectResponse
		outputFormat string
		expectError  bool
	}{
		{
			name: "success table format",
			response: &pb.MigrateObjectResponse{
				Key:           "mybucket/mykey",
				Success:       true,
				OldBackend:    "old-backend",
				NewBackend:    "new-backend",
				SourceDeleted: true,
				Verified:      true,
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "failure table format",
			response: &pb.MigrateObjectResponse{
				Key:     "mybucket/mykey",
				Success: false,
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "json format",
			response: &pb.MigrateObjectResponse{
				Key:        "mybucket/mykey",
				Success:    true,
				OldBackend: "source",
				NewBackend: "dest",
			},
			outputFormat: "json",
			expectError:  false,
		},
		{
			name: "yaml format",
			response: &pb.MigrateObjectResponse{
				Key:           "mybucket/mykey",
				Success:       true,
				OldBackend:    "old",
				NewBackend:    "new",
				SourceDeleted: false,
				Verified:      true,
			},
			outputFormat: "yaml",
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objectOutputFormat = tt.outputFormat
			err := formatObjectMigrateResponse(tt.response)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFormatObjectStatsResponse(t *testing.T) {
	tests := []struct {
		name         string
		response     *pb.ListBackendsResponse
		outputFormat string
		expectError  bool
	}{
		{
			name: "table format with backends",
			response: &pb.ListBackendsResponse{
				Backends: []*pb.BackendInfo{
					{
						Name:    "backend-1",
						Type:    pb.BackendType_BACKEND_TYPE_LOCAL,
						Enabled: true,
						Healthy: true,
						Tier:    pb.StorageTier_STORAGE_TIER_HOT,
					},
					{
						Name:    "backend-2",
						Type:    pb.BackendType_BACKEND_TYPE_S3,
						Enabled: true,
						Healthy: true,
						Tier:    pb.StorageTier_STORAGE_TIER_COLD,
					},
				},
				Total: 2,
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name: "json format",
			response: &pb.ListBackendsResponse{
				Backends: []*pb.BackendInfo{
					{Name: "backend-1", Enabled: true},
				},
				Total: 1,
			},
			outputFormat: "json",
			expectError:  false,
		},
		{
			name: "yaml format",
			response: &pb.ListBackendsResponse{
				Backends: []*pb.BackendInfo{},
				Total:    0,
			},
			outputFormat: "yaml",
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objectOutputFormat = tt.outputFormat
			err := formatObjectStatsResponse(tt.response)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFormatObjectLifecycleResponse(t *testing.T) {
	tests := []struct {
		name         string
		bucket       string
		response     *pb.GetObjectPolicyResponse
		outputFormat string
		expectError  bool
	}{
		{
			name:   "table format with policy",
			bucket: "mybucket",
			response: &pb.GetObjectPolicyResponse{
				Key: "mybucket/mykey",
				Policy: &pb.ObjectPolicy{
					Version:   1,
					UpdatedAt: timestamppb.Now(),
				},
			},
			outputFormat: "table",
			expectError:  false,
		},
		{
			name:   "json format",
			bucket: "mybucket",
			response: &pb.GetObjectPolicyResponse{
				Key: "mybucket/mykey",
				Policy: &pb.ObjectPolicy{
					Version: 1,
				},
			},
			outputFormat: "json",
			expectError:  false,
		},
		{
			name:   "yaml format",
			bucket: "mybucket",
			response: &pb.GetObjectPolicyResponse{
				Key:    "mybucket/mykey",
				Policy: &pb.ObjectPolicy{},
			},
			outputFormat: "yaml",
			expectError:  false,
		},
		{
			name:   "nil policy",
			bucket: "mybucket",
			response: &pb.GetObjectPolicyResponse{
				Key:    "mybucket/mykey",
				Policy: nil,
			},
			outputFormat: "table",
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objectOutputFormat = tt.outputFormat
			err := formatObjectLifecycleResponse(tt.bucket, tt.response)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// =============================================================================
// Helper Function Tests
// =============================================================================

func TestObjectTruncateString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{
			name:     "string shorter than max",
			input:    "short",
			maxLen:   10,
			expected: "short",
		},
		{
			name:     "string equal to max",
			input:    "exactlen10",
			maxLen:   10,
			expected: "exactlen10",
		},
		{
			name:     "string longer than max",
			input:    "this is a very long string",
			maxLen:   10,
			expected: "this is...",
		},
		{
			name:     "empty string",
			input:    "",
			maxLen:   10,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := objectTruncateString(tt.input, tt.maxLen)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestObjectFormatStorageSize(t *testing.T) {
	tests := []struct {
		name     string
		size     int64
		expected string
	}{
		{
			name:     "bytes",
			size:     500,
			expected: "500 B",
		},
		{
			name:     "kilobytes",
			size:     1024,
			expected: "1.0 KB",
		},
		{
			name:     "megabytes",
			size:     1048576,
			expected: "1.0 MB",
		},
		{
			name:     "gigabytes",
			size:     1073741824,
			expected: "1.0 GB",
		},
		{
			name:     "terabytes",
			size:     1099511627776,
			expected: "1.0 TB",
		},
		{
			name:     "zero bytes",
			size:     0,
			expected: "0 B",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := objectFormatStorageSize(tt.size)
			assert.Equal(t, tt.expected, result)
		})
	}
}
