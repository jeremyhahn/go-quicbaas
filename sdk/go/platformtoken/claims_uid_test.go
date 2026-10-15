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

package platformtoken

import (
	"encoding/json"
	"strings"
	"testing"
)

// The uid claim carries the qrdb user Snowflake as a decimal string, so an id
// above 2^53 survives a JWT round trip that a float64 decoder would round.
func TestClaimsUIDRoundTripsAboveFloat64Range(t *testing.T) {
	const uid uint64 = 1<<53 + 1
	data, err := json.Marshal(&Claims{UserID: uid})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(data), `"uid":"9007199254740993"`) {
		t.Fatalf("encoded claims %s, want the uid as a decimal string", data)
	}

	var back Claims
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if back.UserID != uid {
		t.Errorf("round-tripped uid = %d, want %d", back.UserID, uid)
	}
}

// A token without a user (a service token) omits the claim entirely.
func TestClaimsUIDOmittedWhenZero(t *testing.T) {
	data, err := json.Marshal(&Claims{})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(data), `"uid"`) {
		t.Errorf("encoded claims %s, want no uid claim for user id 0", data)
	}
}

func TestClaimsUIDRefusesMalformedValues(t *testing.T) {
	for name, payload := range map[string]string{
		"non-numeric": `{"uid":"alice"}`,
		"negative":    `{"uid":"-1"}`,
		"overflow":    `{"uid":"18446744073709551616"}`,
		"bare number": `{"uid":9007199254740993}`,
	} {
		t.Run(name, func(t *testing.T) {
			var claims Claims
			if err := json.Unmarshal([]byte(payload), &claims); err == nil {
				t.Errorf("Unmarshal(%s) = uid %d, want an error", payload, claims.UserID)
			}
		})
	}
}
