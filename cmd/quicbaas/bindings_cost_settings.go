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
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// GetCostSettings returns the tenant's runtime cost settings — the electricity
// rate and engine draws its local inference is costed from — and the rates in
// force (go-quicbaas#1058). The request and response are the SDK types, as the
// TypeScript Wails client passes and expects them.
func (b *OrchestratorBinding) GetCostSettings(req *orchestratorSDK.GetCostSettingsRequest) (*orchestratorSDK.CostSettings, error) {
	if req == nil {
		return nil, NewGUIRequestRequiredError("OrchestratorBinding.GetCostSettings")
	}
	client, err := b.conn.loadClient()
	if err != nil {
		return nil, err
	}
	resp, err := client.Orchestrator().GetCostSettings(b.ctx, req)
	if err != nil {
		return nil, NewGUITransportError("OrchestratorBinding.GetCostSettings", err)
	}
	return resp, nil
}

// SetCostSettings replaces the tenant's runtime cost settings.
func (b *OrchestratorBinding) SetCostSettings(req *orchestratorSDK.SetCostSettingsRequest) (*orchestratorSDK.CostSettings, error) {
	if req == nil {
		return nil, NewGUIRequestRequiredError("OrchestratorBinding.SetCostSettings")
	}
	client, err := b.conn.loadClient()
	if err != nil {
		return nil, err
	}
	resp, err := client.Orchestrator().SetCostSettings(b.ctx, req)
	if err != nil {
		return nil, NewGUITransportError("OrchestratorBinding.SetCostSettings", err)
	}
	return resp, nil
}
