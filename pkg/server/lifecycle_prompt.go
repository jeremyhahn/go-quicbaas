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

package server

import (
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// attachPromptService mounts the prompt service over the store and resolver
// the boot reconcile uses. A node with no qrdb store has no prompt store, so
// the service stays nil and every transport answers 503 / Unavailable.
func (s *Server) attachPromptService(svc service.QuicBaaS) error {
	resolver, err := s.promptResolver()
	if err != nil {
		return err
	}
	if s.promptStore == nil {
		return nil
	}
	prompts, err := service.NewPromptService(service.PromptServiceConfig{
		Store:    s.promptStore,
		Resolver: resolver,
		Projects: svc.Project(),
	})
	if err != nil {
		return err
	}
	svc.SetPromptService(prompts)
	return nil
}
