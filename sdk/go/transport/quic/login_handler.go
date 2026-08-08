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
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	sdktransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
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
		// The server encodes service.FinishLoginResult, whose embedded qrdb
		// response fields are promoted into the same flat JSON object, so one
		// decode fills both halves.
		out := sdktransport.FinishLoginResponse{FinishLoginResponse: &qrdbtransport.FinishLoginResponse{}}
		if err := DecodePayload(payload, &out); err != nil {
			return nil, err
		}
		return &out, nil
	default:
		return nil, errors.NewInvalidInput("quic.decodeLoginResponse",
			"unknown login method: "+method)
	}
}

// isSessionTokenMethod reports whether the (service, method) pair is one of the
// session-token lifecycle verbs. Their responses are service.*Response structs
// encoded with Go field names, so they need a typed decode rather than the
// generic map passthrough (a raw map fails the caller's type assertion).
func isSessionTokenMethod(svc, method string) bool {
	return (svc == "quicbaas.Auth" || svc == "Auth") && method == "RefreshToken"
}

// decodeSessionTokenResponse decodes a session-token lifecycle response into
// the SDK type every other transport returns.
func decodeSessionTokenResponse(method string, payload []byte) (interface{}, error) {
	switch method {
	case "RefreshToken":
		var out service.RefreshTokenResponse
		if err := DecodePayload(payload, &out); err != nil {
			return nil, err
		}
		return &service.ExtendedRefreshTokenResponse{
			AccessToken:  out.AccessToken,
			ExpiresAt:    time.Unix(out.ExpiresAt, 0),
			TokenType:    out.TokenType,
			RefreshToken: out.RefreshToken,
		}, nil
	default:
		return nil, errors.NewInvalidInput("quic.decodeSessionTokenResponse",
			"unknown session token method: "+method)
	}
}
