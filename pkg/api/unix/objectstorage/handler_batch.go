//go:build unix

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

package objectstorage

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// handleBatchPutObjects handles the BatchPutObjects operation.
func (h *Handler) handleBatchPutObjects(ctx context.Context, reqData []byte) ([]byte, error) {
	var req BatchPutRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	results := make([]*BatchPutResult, 0, len(req.Objects))
	succeeded := 0
	failed := 0

	for _, item := range req.Objects {
		putReq := &service.PutObjectRequest{
			Key:               item.Key,
			Value:             item.Value,
			ContentType:       item.ContentType,
			TenantID:          item.TenantID,
			ShardID:           item.ShardID,
			Tags:              item.Tags,
			TargetBackend:     item.TargetBackend,
			Checksum:          item.Checksum,
			ReplicationPolicy: item.ReplicationPolicy,
			ArchivePolicy:     item.ArchivePolicy,
			RetentionPolicy:   item.RetentionPolicy,
		}

		putResp, err := h.service.PutObject(ctx, putReq)
		if err != nil {
			results = append(results, &BatchPutResult{
				Key:     item.Key,
				Success: false,
				Error:   err.Error(),
			})
			failed++
			continue
		}

		results = append(results, &BatchPutResult{
			Key:       putResp.Key,
			ObjectID:  putResp.ObjectID,
			Checksum:  putResp.Checksum,
			Size:      putResp.Size,
			Backend:   putResp.Backend,
			Version:   putResp.Version,
			CreatedAt: putResp.CreatedAt,
			Success:   true,
		})
		succeeded++
	}

	resp := &BatchPutResponse{
		Results:   results,
		Succeeded: succeeded,
		Failed:    failed,
	}

	return EncodePayload(resp)
}

// handleBatchGetObjects handles the BatchGetObjects operation.
func (h *Handler) handleBatchGetObjects(ctx context.Context, reqData []byte) ([]byte, error) {
	var req BatchGetRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	results := make([]*BatchGetResult, 0, len(req.Keys))
	succeeded := 0
	failed := 0

	for _, key := range req.Keys {
		getReq := &service.GetObjectRequest{
			Key:             key,
			TenantID:        req.TenantID,
			IncludeMetadata: req.IncludeMetadata,
		}

		getResp, err := h.service.GetObject(ctx, getReq)
		if err != nil {
			results = append(results, &BatchGetResult{
				Key:     key,
				Success: false,
				Error:   err.Error(),
			})
			failed++
			continue
		}

		results = append(results, &BatchGetResult{
			Key:         getResp.Key,
			Value:       getResp.Value,
			ContentType: getResp.ContentType,
			Size:        getResp.Size,
			Checksum:    getResp.Checksum,
			Version:     getResp.Version,
			Tags:        getResp.Tags,
			TenantID:    getResp.TenantID,
			CreatedAt:   getResp.CreatedAt,
			ModifiedAt:  getResp.ModifiedAt,
			Backend:     getResp.Backend,
			Success:     true,
		})
		succeeded++
	}

	resp := &BatchGetResponse{
		Results:   results,
		Succeeded: succeeded,
		Failed:    failed,
	}

	return EncodePayload(resp)
}

// handleBatchDeleteObjects handles the BatchDeleteObjects operation.
func (h *Handler) handleBatchDeleteObjects(ctx context.Context, reqData []byte) ([]byte, error) {
	var req BatchDeleteRequest
	if err := DecodePayload(reqData, &req); err != nil {
		return nil, err
	}

	results := make([]*BatchDeleteResult, 0, len(req.Keys))
	succeeded := 0
	failed := 0

	for _, key := range req.Keys {
		delReq := &service.DeleteObjectRequest{
			Key:      key,
			TenantID: req.TenantID,
		}

		delResp, err := h.service.DeleteObject(ctx, delReq)
		if err != nil {
			results = append(results, &BatchDeleteResult{
				Key:     key,
				Success: false,
				Error:   err.Error(),
			})
			failed++
			continue
		}

		results = append(results, &BatchDeleteResult{
			Key:       delResp.Key,
			Size:      delResp.Size,
			Timestamp: delResp.Timestamp,
			Success:   delResp.Success,
		})
		succeeded++
	}

	resp := &BatchDeleteResponse{
		Results:   results,
		Succeeded: succeeded,
		Failed:    failed,
	}

	return EncodePayload(resp)
}
