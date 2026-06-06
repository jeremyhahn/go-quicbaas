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

package quic

import (
	"encoding/json"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// barrierServicePrefix is the SDK-wide service name prefix used for QuicBaaS
// barrier operations dispatched over QUIC.
const barrierServicePrefix = "quicbaas.Barrier"

// isBarrierService reports whether svc names the QuicBaaS Barrier service.
// Barrier methods multiplex over the single OpBarrier (0xC0) wire opcode
// with the SubOpBarrier* code prepended to the JSON payload.
func isBarrierService(svc string) bool {
	return strings.HasPrefix(svc, barrierServicePrefix) || svc == "Barrier"
}

// barrierResponseConverter is the per-method response converter signature.
// Each function receives raw JSON bytes from the wire and returns the typed
// SDK value (or a generic map[string]interface{} fallback).
type barrierResponseConverter func(data []byte) (interface{}, error)

// barrierResponseConverters maps Barrier method names to response decoders
// for O(1) dispatch.
//
// Today every barrier response is JSON-decoded by the caller into the
// concrete BarrierStatusResponse / ShamirInitResponse / etc. struct exported
// by pkg/api/quic. The SDK transport therefore returns the raw decoded
// map[string]interface{} and lets the higher-level SDK service translate it
// — exactly the pattern used by the orchestrator handler before its
// per-method converters landed.
var barrierResponseConverters = map[string]barrierResponseConverter{
	// Intentionally empty: every barrier method currently flows through the
	// generic map[string]interface{} decode path. Per-method converters can
	// be added here as the SDK service layer needs typed responses.
}

// decodeBarrierResponse decodes raw wire bytes into the appropriate SDK
// barrier type. Falls back to a generic JSON object decode when no per-method
// converter is registered.
func decodeBarrierResponse(method string, data []byte) (interface{}, error) {
	if len(data) == 0 {
		return nil, nil
	}
	if converter, ok := barrierResponseConverters[method]; ok {
		return converter(data)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.decodeBarrierResponse", "failed to decode response")
	}
	return decoded, nil
}
