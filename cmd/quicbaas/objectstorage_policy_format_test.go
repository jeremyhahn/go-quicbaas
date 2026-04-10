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

// Package main provides tests for objectstorage format functions that were
// missing invalid format error path coverage: formatObjectPolicyResponse
// and formatObjectSetPolicyResponse.
package main

import (
	"testing"

	"github.com/stretchr/testify/assert"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
)

func TestFormatObjectPolicyResponse_InvalidFormat(t *testing.T) {
	saved := objectOutputFormat
	defer func() { objectOutputFormat = saved }()
	objectOutputFormat = "INVALID_FORMAT_XYZ"

	resp := &pb.GetObjectPolicyResponse{
		Key: "bucket/key",
	}
	err := formatObjectPolicyResponse(resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "INVALID_FORMAT_XYZ")
}

func TestFormatObjectSetPolicyResponse_InvalidFormat(t *testing.T) {
	saved := objectOutputFormat
	defer func() { objectOutputFormat = saved }()
	objectOutputFormat = "INVALID_FORMAT_XYZ"

	resp := &pb.SetObjectPolicyResponse{
		Key:     "bucket/key",
		Version: 1,
	}
	err := formatObjectSetPolicyResponse(resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "INVALID_FORMAT_XYZ")
}
