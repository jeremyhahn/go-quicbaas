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

package quic

import (
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// isLoginCeremonyMethod reports whether the (service, method) pair is one of
// the unified login ceremony verbs routed over the dedicated QUIC opcodes
// (OpAuthBeginLogin / OpAuthFinishLogin). The request structs encode directly
// via JSON (they carry json tags), so no request transform is needed; only the
// response must be decoded into the canonical qrdb transport types.
func isLoginCeremonyMethod(svc, method string) bool {
	return svc == "quicbaas.Auth" && (method == "BeginLogin" || method == "FinishLogin")
}

// decodeLoginResponse decodes a login-ceremony response payload into the
// canonical go-qrdb transport response type for the given method.
func decodeLoginResponse(method string, payload []byte) (interface{}, error) {
	switch method {
	case "BeginLogin":
		var out qrdbtransport.BeginLoginResponse
		if err := DecodePayload(payload, &out); err != nil {
			return nil, err
		}
		return &out, nil
	case "FinishLogin":
		var out qrdbtransport.FinishLoginResponse
		if err := DecodePayload(payload, &out); err != nil {
			return nil, err
		}
		return &out, nil
	default:
		return nil, errors.NewInvalidInput("quic.decodeLoginResponse",
			"unknown login method: "+method)
	}
}
