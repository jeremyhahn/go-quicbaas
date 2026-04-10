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

// launchGUI starts the Wails desktop application with connection and
// orchestrator bindings exposed to the frontend via IPC.
func launchGUI(log *logger.Logger, logFile string) error {
	conn := NewConnectionBinding(log)
	orch := NewOrchestratorBinding(log, conn)

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
		},
		OnShutdown: func(ctx context.Context) {
			orch.shutdown(ctx)
			conn.shutdown(ctx)
		},
		Bind: []interface{}{conn, orch},
	})
}
