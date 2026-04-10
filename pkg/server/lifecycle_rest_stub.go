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

//go:build !rest

package server

import (
	"context"
	"errors"

	restapi "github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// ErrRESTNotCompiled is returned when REST methods are called but
// the rest build tag was not specified during compilation.
var ErrRESTNotCompiled = errors.New("rest support not compiled - build with -tags rest")

// REST seam injection points (stub versions for non-REST builds).
// These use the same variable names and function signatures as the real
// REST build so that shared test helpers (e.g., saveAndRestoreSeams)
// compile regardless of build tags.
//
//nolint:unused // newREST is used in rest-tagged builds and tests
var newREST = func(_ service.QuicBaaS, _ *restapi.Config, _ *logger.Logger) (*restapi.Server, error) {
	return nil, ErrRESTNotCompiled
}

var restShutdownFn = func(_ *restapi.Server, _ context.Context) error {
	return ErrRESTNotCompiled
}

// startRESTServer is a no-op when REST is not compiled.
func (s *Server) startRESTServer() error {
	s.logger.Info("REST server not available (build with -tags rest to enable)")
	return nil
}

// stopRESTServer delegates to the restShutdownFn seam when a REST server
// instance is present. This mirrors the real implementation so that
// test seam overrides work correctly even without the rest build tag.
func (s *Server) stopRESTServer(ctx context.Context) error {
	if s.restServer == nil {
		return nil
	}
	return restShutdownFn(s.restServer, ctx)
}
