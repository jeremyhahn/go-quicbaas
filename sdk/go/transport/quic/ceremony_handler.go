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

// ceremonyServicePrefix is the SDK-wide service name prefix used for
// QuicBaaS ceremony operations dispatched over QUIC.
const ceremonyServicePrefix = "quicbaas.Ceremony"

// isCeremonyService reports whether svc names the QuicBaaS Ceremony service.
// Ceremony methods multiplex over the single OpCeremony (0xC1) wire opcode
// with the SubOpCeremony* code prepended to the JSON payload.
func isCeremonyService(svc string) bool {
	return strings.HasPrefix(svc, ceremonyServicePrefix) || svc == "Ceremony"
}

// ceremonyResponseConverter is the per-method response converter signature.
type ceremonyResponseConverter func(data []byte) (interface{}, error)

// ceremonyResponseConverters maps Ceremony method names to response decoders
// for O(1) dispatch.
//
// Today every ceremony response is JSON-decoded by the caller into the
// concrete CeremonyInitResponse / CeremonyStatusResponse / etc. struct
// exported by pkg/api/quic. Mirrors the barrier handler convention.
var ceremonyResponseConverters = map[string]ceremonyResponseConverter{
	// Intentionally empty: every ceremony method currently flows through the
	// generic map[string]interface{} decode path. Per-method converters can
	// be added here as the SDK service layer needs typed responses.
}

// decodeCeremonyResponse decodes raw wire bytes into the appropriate SDK
// ceremony type. Falls back to a generic JSON object decode when no
// per-method converter is registered.
func decodeCeremonyResponse(method string, data []byte) (interface{}, error) {
	if len(data) == 0 {
		return nil, nil
	}
	if converter, ok := ceremonyResponseConverters[method]; ok {
		return converter(data)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.decodeCeremonyResponse", "failed to decode response")
	}
	return decoded, nil
}
