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
	"fmt"
	"sync/atomic"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// HandlerFunc is the signature for request handlers.
type HandlerFunc func(ctx context.Context, req []byte) ([]byte, error)

// Handler handles ObjectStorage Unix socket requests.
type Handler struct {
	service      service.ObjectStorageService
	adminService service.ObjectStorageAdminService
	objectStores *objectStoreRegistry
	handlers     map[byte]HandlerFunc
	logger       interfaces.Logger
	closed       atomic.Bool
}

// NewHandler creates a new ObjectStorage Unix socket handler.
func NewHandler(
	svc service.ObjectStorageService,
	adminSvc service.ObjectStorageAdminService,
	logger interfaces.Logger,
) (*Handler, error) {
	if svc == nil {
		return nil, api.NewRequiredFieldError("object storage service")
	}

	if logger == nil {
		return nil, api.NewRequiredFieldError("logger")
	}

	h := &Handler{
		service:      svc,
		adminService: adminSvc,
		objectStores: newObjectStoreRegistry(),
		logger:       logger.WithField("component", "unix_objectstorage_handler"),
	}

	h.registerHandlers()

	h.logger.Info("unix objectstorage handler initialized")

	return h, nil
}

// registerHandlers registers all operation handlers using map-based dispatch.
func (h *Handler) registerHandlers() {
	h.handlers = map[byte]HandlerFunc{
		// CRUD Operations
		OpPutObject:    h.handlePutObject,
		OpGetObject:    h.handleGetObject,
		OpDeleteObject: h.handleDeleteObject,
		OpHeadObject:   h.handleHeadObject,

		// Listing Operations
		OpListObjects:  h.handleListObjects,
		OpListByTag:    h.handleListByTag,
		OpListByTenant: h.handleListByTenant,

		// Copy Operations
		OpCopyObject: h.handleCopyObject,

		// Replication & Migration Operations
		OpReplicateObject: h.handleReplicateObject,
		OpMigrateObject:   h.handleMigrateObject,

		// Archive & Restore Operations
		OpArchiveObject: h.handleArchiveObject,
		OpRestoreObject: h.handleRestoreObject,

		// Verification Operations
		OpVerifyObject: h.handleVerifyObject,

		// Policy Operations
		OpSetObjectPolicy: h.handleSetObjectPolicy,
		OpGetObjectPolicy: h.handleGetObjectPolicy,

		// Backend Management Operations
		OpListBackends:     h.handleListBackends,
		OpGetBackendHealth: h.handleGetBackendHealth,

		// Tenant Management Operations
		OpGetTenantUsage:  h.handleGetTenantUsage,
		OpGetTenantQuotas: h.handleGetTenantQuotas,
		OpSetTenantQuotas: h.handleSetTenantQuotas,

		// Batch Operations
		OpBatchPutObjects:    h.handleBatchPutObjects,
		OpBatchGetObjects:    h.handleBatchGetObjects,
		OpBatchDeleteObjects: h.handleBatchDeleteObjects,

		// Admin Backend Operations
		OpAdminCreateBackend:    h.handleAdminCreateBackend,
		OpAdminUpdateBackend:    h.handleAdminUpdateBackend,
		OpAdminDeleteBackend:    h.handleAdminDeleteBackend,
		OpAdminSetBackendStatus: h.handleAdminSetBackendStatus,

		// Admin Statistics Operations
		OpAdminGetStorageStats: h.handleAdminGetStorageStats,

		// Admin GC Operations
		OpAdminRunGC:         h.handleAdminRunGC,
		OpAdminGetGCProgress: h.handleAdminGetGCProgress,

		// Admin Repair Operations
		OpAdminRepairObject: h.handleAdminRepairObject,

		// Admin Compaction Operations
		OpAdminCompactBackend: h.handleAdminCompactBackend,

		// Object Transfer Operations
		OpFetchObject:        h.handleFetchObject,
		OpStreamFetchObjects: h.handleStreamFetchObjects,
	}
}

// Handle processes a request and returns a response.
func (h *Handler) Handle(ctx context.Context, opcode byte, req []byte) ([]byte, error) {
	if h.closed.Load() {
		return nil, api.NewHandlerClosedError("unix.objectstorage.Handle")
	}

	handler, ok := h.handlers[opcode]
	if !ok {
		return nil, api.NewUnknownOpcodeError("unix.objectstorage.Handle", opcode)
	}

	h.logger.WithFields(map[string]interface{}{
		"opcode": fmt.Sprintf("0x%02X", opcode),
		"op":     OpcodeToString(opcode),
	}).Debug("handling request")

	return handler(ctx, req)
}

// HandleRequest processes a full Request struct and returns a Response.
func (h *Handler) HandleRequest(ctx context.Context, req *Request) *Response {
	if req == nil {
		return &Response{
			Status:  StatusInvalidRequest,
			Payload: encodeErrorResponse("request cannot be nil"),
		}
	}

	payload, err := h.Handle(ctx, req.Opcode, req.Payload)
	if err != nil {
		return &Response{
			Status:  ErrorToStatus(err),
			Payload: encodeErrorResponse(err.Error()),
		}
	}

	return &Response{
		Status:  StatusOK,
		Payload: payload,
	}
}

// Close closes the handler.
func (h *Handler) Close() error {
	if h.closed.Swap(true) {
		return nil
	}
	h.logger.Info("unix objectstorage handler closed")
	return nil
}

// encodeErrorResponse encodes an error message as a response payload.
func encodeErrorResponse(msg string) []byte {
	resp := &ErrorResponse{
		Error: msg,
	}
	data, _ := EncodePayload(resp)
	return data
}

// Service returns the object storage service.
func (h *Handler) Service() service.ObjectStorageService {
	return h.service
}

// AdminService returns the object storage admin service.
func (h *Handler) AdminService() service.ObjectStorageAdminService {
	return h.adminService
}

// IsAdminOperation returns true if the opcode is an admin operation.
func IsAdminOperation(opcode byte) bool {
	return opcode >= OpAdminCreateBackend && opcode <= OpAdminCompactBackend
}

// RequiresAdminService returns true if the opcode requires the admin service.
func RequiresAdminService(opcode byte) bool {
	return IsAdminOperation(opcode)
}
