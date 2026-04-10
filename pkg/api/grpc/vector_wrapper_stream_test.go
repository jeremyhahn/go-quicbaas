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

package grpc

import (
	"testing"

	"github.com/stretchr/testify/assert"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// TestVectorDBWrapper_BatchUpsertVectors tests the BatchUpsertVectors streaming delegation.
func TestVectorDBWrapper_BatchUpsertVectors_NilServer(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	server := &Server{
		logger:  log,
		nodeID:  1,
		version: "1.0.0",
	}

	wrapper := NewVectorDBWrapper(server)

	req := &pb.BatchUpsertVectorsRequest{
		Vectors: []*pb.VectorEntity{
			{Id: "vec-1", Namespace: "test-ns", Vector: []float32{1.0, 2.0, 3.0}},
		},
	}

	// This will return an error since vectorService is nil
	err := wrapper.BatchUpsertVectors(req, nil)

	// The wrapper delegates to server which will fail due to nil vectorService
	assert.Error(t, err)
}

// TestVectorDBWrapper_SearchSimilar tests the SearchSimilar streaming delegation.
func TestVectorDBWrapper_SearchSimilar_NilServer(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	server := &Server{
		logger:  log,
		nodeID:  1,
		version: "1.0.0",
	}

	wrapper := NewVectorDBWrapper(server)

	req := &pb.SearchSimilarRequest{
		Namespace:   "test-ns",
		QueryVector: []float32{1.0, 2.0, 3.0},
		K:           10,
	}

	err := wrapper.SearchSimilar(req, nil)

	assert.Error(t, err)
}

// TestVectorDBWrapper_SearchByID tests the SearchByID streaming delegation.
func TestVectorDBWrapper_SearchByID_NilServer(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	server := &Server{
		logger:  log,
		nodeID:  1,
		version: "1.0.0",
	}

	wrapper := NewVectorDBWrapper(server)

	req := &pb.SearchByIDRequest{
		Namespace: "test-ns",
		Id:        "vec-1",
		K:         10,
	}

	err := wrapper.SearchByID(req, nil)

	assert.Error(t, err)
}

// TestVectorDBWrapper_ListVectors tests the ListVectors streaming delegation.
func TestVectorDBWrapper_ListVectors_NilServer(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	server := &Server{
		logger:  log,
		nodeID:  1,
		version: "1.0.0",
	}

	wrapper := NewVectorDBWrapper(server)

	req := &pb.ListVectorsRequest{
		Namespace: "test-ns",
		PageSize:  100,
	}

	err := wrapper.ListVectors(req, nil)

	assert.Error(t, err)
}

// TestVectorAdminWrapper_ExportVectors tests the ExportVectors streaming delegation.
func TestVectorAdminWrapper_ExportVectors_NilServer(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	server := &Server{
		logger:  log,
		nodeID:  1,
		version: "1.0.0",
	}

	wrapper := NewVectorAdminWrapper(server)

	req := &pb.ExportVectorsRequest{
		Namespace: "test-ns",
	}

	err := wrapper.ExportVectors(req, nil)

	assert.Error(t, err)
}

// TestVectorAdminWrapper_ImportVectors tests the ImportVectors streaming delegation.
func TestVectorAdminWrapper_ImportVectors_NilServer(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	server := &Server{
		logger:  log,
		nodeID:  1,
		version: "1.0.0",
	}

	wrapper := NewVectorAdminWrapper(server)

	err := wrapper.ImportVectors(nil)

	assert.Error(t, err)
}
