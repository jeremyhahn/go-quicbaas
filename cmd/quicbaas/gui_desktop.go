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

//go:build desktop

package main

import (
	"context"
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

//go:embed all:frontend/dist
var assets embed.FS

// launchGUI starts the Wails desktop application with the full set of
// bindings the Svelte frontend drives: connection management,
// orchestrator ops, session event history + subscriptions, agent
// registry queries, and embedded-server control for standalone mode.
func launchGUI(log *logger.Logger, logFile string) error {
	conn := NewConnectionBinding(log)
	orch := NewOrchestratorBinding(log, conn)
	sessions := NewSessionBinding(log, conn)
	agents := NewAgentBinding(log, conn)
	embedded := NewEmbeddedServerBinding(log)
	agentDeploy := NewAgentDeployBinding(log, conn)
	sshKeys := NewSSHKeysBinding(log)

	return wails.Run(&options.App{
		Title:  "QuicBaaS",
		Width:  1600,
		Height: 1000,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup: func(ctx context.Context) {
			conn.startup(ctx)
			orch.startup(ctx)
			sessions.startup(ctx)
			agents.startup(ctx)
			embedded.startup(ctx)
			agentDeploy.startup(ctx)
			sshKeys.startup(ctx)
		},
		OnShutdown: func(ctx context.Context) {
			sshKeys.shutdown(ctx)
			agentDeploy.shutdown(ctx)
			embedded.shutdown(ctx)
			agents.shutdown(ctx)
			sessions.shutdown(ctx)
			orch.shutdown(ctx)
			conn.shutdown(ctx)
		},
		Bind: []interface{}{conn, orch, sessions, agents, embedded, agentDeploy, sshKeys},
	})
}
