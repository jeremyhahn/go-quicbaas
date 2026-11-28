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
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

// xkmsModuleSessionOptions builds the session options of the hosted xkms
// login module from quicbaas config, so its tokens carry the same issuer
// and lifetimes as the OIDC provider's and its login rate is the operator's.
func (s *Server) xkmsModuleSessionOptions() *xkmssdk.ServiceModuleSessionOptions {
	return &xkmssdk.ServiceModuleSessionOptions{
		Issuer:          issuerURLFromConfig(s.config),
		AccessTTL:       s.config.Auth.OIDCAccessTokenTTL(),
		RefreshTTL:      s.config.Auth.OIDCRefreshTokenTTL(),
		LoginRatePerMin: s.config.IAM.LoginRatePerMin,
	}
}
