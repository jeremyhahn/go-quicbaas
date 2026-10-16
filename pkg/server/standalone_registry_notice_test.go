// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// A standalone boot names the registry's typed refusal once at startup, so an
// operator learns it from the log rather than from the first failed call.
func TestStandaloneRegistryNotice_StandaloneBoot(t *testing.T) {
	for _, mode := range []string{"", config.ServerModeStandalone} {
		t.Run("mode="+mode, func(t *testing.T) {
			s := &Server{config: &config.Config{Server: config.ServerConfig{Mode: mode}}}
			notice := s.standaloneRegistryNotice()
			if !strings.Contains(notice, "RegistryRequiresRaftError") {
				t.Fatalf("notice = %q, want it to name RegistryRequiresRaftError", notice)
			}
		})
	}
}

// A cluster-like boot runs a raft node that serves the registry, so it logs
// nothing about it.
func TestStandaloneRegistryNotice_ClusterBoot(t *testing.T) {
	for _, mode := range []string{config.ServerModeCluster, config.ServerModeDevCluster} {
		t.Run("mode="+mode, func(t *testing.T) {
			s := &Server{config: &config.Config{Server: config.ServerConfig{Mode: mode}}}
			if notice := s.standaloneRegistryNotice(); notice != "" {
				t.Fatalf("notice = %q on a %s boot, want none", notice, mode)
			}
		})
	}
}
