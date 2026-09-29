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

//go:build desktop

package main

import (
	sdlcSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

// GetWorkerImageStatus reports the Software Factory worker image refresh on the
// node: whether its loop serves, whether a pass is running or queued, and what
// the last pass found and did. It reaches the SDK's SDLC client — the one
// canonical entry point — and returns the SDK type unreshaped, so the desktop
// UI renders the same record every other transport serves.
func (b *OrchestratorBinding) GetWorkerImageStatus() (*sdlcSDK.WorkerImageStatus, error) {
	client, err := b.conn.loadClient()
	if err != nil {
		return nil, err
	}
	status, err := client.SDLC().WorkerImageStatus(b.ctx)
	if err != nil {
		return nil, NewGUITransportError("OrchestratorBinding.GetWorkerImageStatus", err)
	}
	return status, nil
}

// RefreshWorkerImage queues a full worker image refresh pass — the UI's "Check
// for updates now" — and returns the status it left behind. The pass runs on
// the daemon; the UI polls GetWorkerImageStatus for its outcome.
func (b *OrchestratorBinding) RefreshWorkerImage() (*sdlcSDK.WorkerImageStatus, error) {
	client, err := b.conn.loadClient()
	if err != nil {
		return nil, err
	}
	status, err := client.SDLC().RefreshWorkerImage(b.ctx)
	if err != nil {
		return nil, NewGUITransportError("OrchestratorBinding.RefreshWorkerImage", err)
	}
	return status, nil
}
