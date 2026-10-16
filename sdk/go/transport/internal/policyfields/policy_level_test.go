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

package policyfields

import (
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

func TestRefusePolicyLevelEffectAndEnabled(t *testing.T) {
	refused := map[string]map[string]interface{}{
		"effect":  {"name": "p", "effect": 1},
		"enabled": {"name": "p", "enabled": false},
	}
	for field, request := range refused {
		t.Run(field, func(t *testing.T) {
			err := Refuse("op", "gRPC", request)
			if !errors.IsInvalidInput(err) {
				t.Fatalf("Refuse() = %v, want INVALID_INPUT", err)
			}
			if !strings.Contains(err.Error(), field) {
				t.Fatalf("Refuse() = %v, want the error to name %q", err, field)
			}
		})
	}
}

func TestRefusePolicyLevelDefaultsPass(t *testing.T) {
	request := map[string]interface{}{"name": "p", "effect": 0, "enabled": true, "spec_yaml": "rules: []"}
	if err := Refuse("op", "gRPC", request); err != nil {
		t.Fatalf("Refuse() = %v, want nil for deny and enabled=true", err)
	}
}
