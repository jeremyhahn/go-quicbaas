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

//go:build !quic

package server

import (
	"errors"

	quicapi "github.com/jeremyhahn/go-quicbaas/pkg/api/quic"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// ErrQUICNotCompiled is returned when QUIC operations are attempted
// without the quic build tag.
var ErrQUICNotCompiled = errors.New("quic support not compiled - build with -tags quic")

// QUIC seam injection points (stub versions for non-QUIC builds).
// These use the same variable names and function signatures as the real
// QUIC build so that shared test helpers (e.g., saveAndRestoreSeams)
// compile regardless of build tags.
//
//nolint:unused // newQUIC is used in quic-tagged builds and tests
var newQUIC = func(_ service.QuicBaaS, _ *quicapi.Config, _ *logger.Logger, _ ...quicapi.ServerOption) (*quicapi.Server, error) {
	return nil, ErrQUICNotCompiled
}

var quicStopFn = func(_ *quicapi.Server) error {
	return ErrQUICNotCompiled
}

// startQUICServer is a stub that logs a message when QUIC is not compiled.
func (s *Server) startQUICServer() error {
	if s.config.Server.QUIC.Enabled {
		s.logger.Warn("QUIC server enabled in config but QUIC support not compiled - build with -tags quic")
	}
	return nil
}

// stopQUICServer delegates to the quicStopFn seam when a QUIC server
// instance is present. This mirrors the real implementation so that
// test seam overrides work correctly even without the quic build tag.
func (s *Server) stopQUICServer() error {
	if s.quicServer == nil {
		return nil
	}
	return quicStopFn(s.quicServer)
}
