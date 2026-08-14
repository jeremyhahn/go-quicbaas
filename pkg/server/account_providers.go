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
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// accountProviderDescriptors translates the orchestration.account_providers
// configuration section into the domain descriptors
// orchestration.NewAccountProviderRegistry merges over its built-ins.
//
// This is the composition root's job and lives here rather than in
// pkg/orchestration: the domain describes providers with ProviderDescriptor
// and knows nothing about how an operator spells them in YAML. Validation of
// the entries themselves (https endpoint, no unflagged shadowing of a
// built-in) has already happened in config.Validator by the time this runs.
func accountProviderDescriptors(configured []config.AccountProviderConfig) []orchestration.ProviderDescriptor {
	descriptors := make([]orchestration.ProviderDescriptor, 0, len(configured))
	for _, provider := range configured {
		descriptors = append(descriptors, orchestration.ProviderDescriptor{
			Provider:          orchestration.AccountProvider(provider.Name),
			DisplayName:       provider.DisplayName,
			TokenEndpoint:     provider.TokenEndpoint,
			ClientID:          provider.ClientID,
			Scopes:            provider.Scopes,
			AccessTokenEnvKey: provider.AccessTokenEnvKey,
		})
	}
	return descriptors
}
