//go:build quic

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

package quic

import (
	"sync"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// handlerIAMServices stores the IAM service for each handler instance (IAM-enabled build).
// This field is set via the WithIAMService option.
var handlerIAMServices sync.Map

// handlerMFAServices stores the MFA service for each handler instance (IAM-enabled build).
// This field is set via the WithMFAService option.
var handlerMFAServices sync.Map

// setIAMService stores the IAM service for a handler instance.
func (h *Handler) setIAMService(svc service.IAMService) {
	if svc != nil {
		handlerIAMServices.Store(h, svc)
	}
}

// getIAMService retrieves the IAM service for this handler.
func (h *Handler) getIAMService() service.IAMService {
	v, ok := handlerIAMServices.Load(h)
	if !ok {
		return nil
	}
	svc, _ := v.(service.IAMService)
	return svc
}

// clearIAMService removes the IAM service association (for cleanup).
func (h *Handler) clearIAMService() {
	handlerIAMServices.Delete(h)
}

// WithIAMService sets the IAM service for the handler.
func WithIAMService(svc service.IAMService) HandlerOption {
	return func(h *Handler) {
		h.setIAMService(svc)
	}
}

// setMFAService stores the MFA service for a handler instance.
func (h *Handler) setMFAService(svc service.MFAManager) {
	if svc != nil {
		handlerMFAServices.Store(h, svc)
	}
}

// getMFAService retrieves the MFA service for this handler.
func (h *Handler) getMFAService() service.MFAManager {
	v, ok := handlerMFAServices.Load(h)
	if !ok {
		return nil
	}
	svc, _ := v.(service.MFAManager)
	return svc
}

// clearMFAService removes the MFA service association (for cleanup).
func (h *Handler) clearMFAService() {
	handlerMFAServices.Delete(h)
}

// WithMFAService sets the MFA service for the handler.
func WithMFAService(svc service.MFAManager) HandlerOption {
	return func(h *Handler) {
		h.setMFAService(svc)
	}
}
