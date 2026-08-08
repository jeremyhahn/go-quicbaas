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

package transport

import (
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// FinishLoginResponse carries the issued OIDC bearer, the resolved identity,
// and the credential that renews the session.
//
// It lives in this shared transport package (rather than in the sdk root)
// because every concrete transport handler — REST, gRPC, QUIC, MCP, embedded —
// must construct it, and those packages cannot import the sdk root without a
// cycle. The sdk root re-exports it as quicbaas.FinishLoginResponse.
//
// It is a QuicBaaS-owned type rather than a re-export of go-qrdb's response:
// qrdb owns the factor ceremony, QuicBaaS owns the OAuth2/OIDC session layered
// on top of it, and the renewal credential belongs to that layer. The qrdb
// response is embedded, so Bearer / Identity / ExpiresAt / FactorID are read
// exactly as before.
type FinishLoginResponse struct {
	*qrdbtransport.FinishLoginResponse

	// RefreshToken renews the session without re-running the factor ceremony.
	// It is opaque, rotated on every redemption, and revocable server-side.
	// Persist it: after a renewal the previous value is dead.
	RefreshToken string `json:"refreshToken,omitempty"`

	// RefreshExpiresAt is the session's absolute deadline. Renewal does not
	// move it; past this instant the factor ceremony must run again.
	RefreshExpiresAt time.Time `json:"refreshExpiresAt,omitempty"`
}
