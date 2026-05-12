//go:build iam

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
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// webauthnFactoryConfigFromOperator translates the operator-supplied
// quicbaas WebAuthn configuration into the factory-level shape consumed
// by the per-tenant xkms WebAuthn services. Defaults that match the
// pre-Phase 5 behaviour (RPID=localhost, RPName=QuicBaaS,
// RPOrigins=https://localhost) are applied only when the operator
// leaves the corresponding field empty — production deployments must
// populate IAMWebAuthnConfig in their configuration file.
func webauthnFactoryConfigFromOperator(c config.IAMWebAuthnConfig) iam.WebAuthnFactoryConfig {
	out := iam.WebAuthnFactoryConfig{
		RPID:                    c.RPID,
		RPDisplayName:           c.RPDisplayName,
		RPOrigins:               append([]string(nil), c.RPOrigins...),
		UserVerification:        c.UserVerification,
		AttestationPreference:   c.AttestationPreference,
		ResidentKeyRequirement:  c.ResidentKeyRequirement,
		AuthenticatorAttachment: c.AuthenticatorAttachment,
		Debug:                   c.Debug,
	}
	if out.RPID == "" {
		out.RPID = "localhost"
	}
	if out.RPDisplayName == "" {
		out.RPDisplayName = "QuicBaaS"
	}
	if len(out.RPOrigins) == 0 {
		out.RPOrigins = []string{"https://localhost"}
	}
	if c.Timeout != "" {
		if d, err := time.ParseDuration(c.Timeout); err == nil {
			out.Timeout = d
		}
	}
	return out
}
