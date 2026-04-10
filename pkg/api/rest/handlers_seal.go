//go:build rest

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

package rest

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"io"
	"net/http"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// HandleSealStatus handles GET /v1/seal/status - Get seal status.
func (s *Server) HandleSealStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	sealService, ok := s.service.(interface {
		Status(ctx context.Context) (*service.SealStatusResponse, error)
	})
	if !ok {
		RespondError(w, r, http.StatusInternalServerError, ErrSealServiceUnavailable)
		return
	}

	resp, err := sealService.Status(ctx)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleSealInit handles POST /v1/seal/init - Initialize seal.
func (s *Server) HandleSealInit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req service.SealInitRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	if req.Threshold <= 0 {
		RespondBadRequest(w, r, "threshold must be greater than 0")
		return
	}

	if req.TotalShares <= 0 {
		RespondBadRequest(w, r, "total shares must be greater than 0")
		return
	}

	if req.Threshold > req.TotalShares {
		RespondBadRequest(w, r, "threshold cannot exceed total shares")
		return
	}

	sealService, ok := s.service.(interface {
		Initialize(ctx context.Context, req *service.SealInitRequest) (*service.SealInitResponse, error)
	})
	if !ok {
		RespondError(w, r, http.StatusInternalServerError, ErrSealServiceUnavailable)
		return
	}

	resp, err := sealService.Initialize(ctx, &req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleUnseal handles POST /v1/seal/unseal - Unseal with key share.
func (s *Server) HandleUnseal(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req service.UnsealRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	if len(req.Share) == 0 {
		RespondBadRequest(w, r, "key share is required")
		return
	}

	sealService, ok := s.service.(interface {
		Unseal(ctx context.Context, req *service.UnsealRequest) (*service.UnsealResponse, error)
	})
	if !ok {
		RespondError(w, r, http.StatusInternalServerError, ErrSealServiceUnavailable)
		return
	}

	resp, err := sealService.Unseal(ctx, &req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleSeal handles POST /v1/seal/seal - Seal database.
func (s *Server) HandleSeal(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	sealService, ok := s.service.(interface {
		Seal(ctx context.Context) error
	})
	if !ok {
		RespondError(w, r, http.StatusInternalServerError, ErrSealServiceUnavailable)
		return
	}

	err := sealService.Seal(ctx)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	resp := map[string]interface{}{
		"sealed":  true,
		"message": "database sealed successfully",
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleRekey handles POST /v1/seal/rekey - Rekey master key.
func (s *Server) HandleRekey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req service.RekeyRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	if req.NewThreshold <= 0 {
		RespondBadRequest(w, r, "new threshold must be greater than 0")
		return
	}

	if req.NewTotalShares <= 0 {
		RespondBadRequest(w, r, "new total shares must be greater than 0")
		return
	}

	if req.NewThreshold > req.NewTotalShares {
		RespondBadRequest(w, r, "new threshold cannot exceed new total shares")
		return
	}

	sealService, ok := s.service.(interface {
		Rekey(ctx context.Context, req *service.RekeyRequest) (*service.RekeyResponse, error)
	})
	if !ok {
		RespondError(w, r, http.StatusInternalServerError, ErrSealServiceUnavailable)
		return
	}

	resp, err := sealService.Rekey(ctx, &req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleGenerateRecoveryKey handles POST /v1/seal/recovery/generate - Generate recovery key.
func (s *Server) HandleGenerateRecoveryKey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req service.GenerateRecoveryKeyRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	if req.RecoveryThreshold <= 0 {
		RespondBadRequest(w, r, "recovery threshold must be greater than 0")
		return
	}

	if req.RecoveryTotalKeys <= 0 {
		RespondBadRequest(w, r, "recovery total keys must be greater than 0")
		return
	}

	if req.RecoveryThreshold > req.RecoveryTotalKeys {
		RespondBadRequest(w, r, "recovery threshold cannot exceed recovery total keys")
		return
	}

	sealService, ok := s.service.(interface {
		GenerateRecoveryKey(ctx context.Context, req *service.GenerateRecoveryKeyRequest) (*service.GenerateRecoveryKeyResponse, error)
	})
	if !ok {
		RespondError(w, r, http.StatusInternalServerError, ErrSealServiceUnavailable)
		return
	}

	resp, err := sealService.GenerateRecoveryKey(ctx, &req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleDeleteRecoveryKey handles DELETE /v1/seal/recovery - Delete recovery key.
func (s *Server) HandleDeleteRecoveryKey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	body, err := io.ReadAll(r.Body)
	if err != nil && !stderrors.Is(err, io.EOF) {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req service.DeleteRecoveryKeyRequest
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			RespondBadRequest(w, r, "invalid JSON body")
			return
		}
	}

	if !req.Confirm {
		RespondBadRequest(w, r, "confirm must be true to delete recovery keys")
		return
	}

	sealService, ok := s.service.(interface {
		DeleteRecoveryKey(ctx context.Context, req *service.DeleteRecoveryKeyRequest) (*service.DeleteRecoveryKeyResponse, error)
	})
	if !ok {
		RespondError(w, r, http.StatusInternalServerError, ErrSealServiceUnavailable)
		return
	}

	resp, err := sealService.DeleteRecoveryKey(ctx, &req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleGenerateRootToken handles POST /v1/seal/root/generate - Generate root token.
func (s *Server) HandleGenerateRootToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req service.GenerateRootTokenRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	if req.Strategy == "" {
		RespondBadRequest(w, r, "strategy is required")
		return
	}

	sealService, ok := s.service.(interface {
		GenerateRootToken(ctx context.Context, req *service.GenerateRootTokenRequest) (*service.GenerateRootTokenResponse, error)
	})
	if !ok {
		RespondError(w, r, http.StatusInternalServerError, ErrSealServiceUnavailable)
		return
	}

	resp, err := sealService.GenerateRootToken(ctx, &req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}
