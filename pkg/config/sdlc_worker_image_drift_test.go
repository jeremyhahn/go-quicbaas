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

package config_test

import (
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/sdlc/workerimage"
)

// TestSDLCWorkerImage_ShippedDefaultsMatchTheModule pins the configuration's
// stated defaults to the module's own, so an operator reading the shipped
// configuration reads what actually runs.
func TestSDLCWorkerImage_ShippedDefaultsMatchTheModule(t *testing.T) {
	shipped := config.Default().SDLC.WorkerImage
	pairs := []struct {
		name            string
		shipped, module any
	}{
		{"repository", shipped.Repository, workerimage.DefaultRepository},
		{"npm_registry_url", shipped.NPMRegistryURL, workerimage.DefaultNPMRegistryURL},
		{"refresh_interval", shipped.RefreshInterval, workerimage.DefaultRefreshInterval},
		{"pass_timeout", shipped.PassTimeout, workerimage.DefaultPassTimeout},
	}
	for _, p := range pairs {
		if p.shipped != p.module {
			t.Errorf("%s: shipped default %v, module default %v", p.name, p.shipped, p.module)
		}
	}
}
