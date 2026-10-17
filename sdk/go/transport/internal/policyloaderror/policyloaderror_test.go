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

package policyloaderror

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCopy_FlaggedPolicyCarriesLoadError(t *testing.T) {
	loadError := map[string]interface{}{
		"reason":       `json: unknown field "bogus"`,
		"unknown_keys": []interface{}{"spec.rules[0].bogus"},
	}
	server := map[string]interface{}{"metadata": map[string]interface{}{}, Key: loadError}
	sdk := map[string]interface{}{"name": "flagged"}

	Copy(server, sdk)

	assert.Equal(t, loadError, sdk[Key])
	assert.Equal(t, "flagged", sdk["name"], "other fields are left alone")
}

func TestCopy_HealthyPolicyGetsNoLoadError(t *testing.T) {
	sdk := map[string]interface{}{"name": "healthy"}

	Copy(map[string]interface{}{"metadata": map[string]interface{}{}}, sdk)

	assert.NotContains(t, sdk, Key)
	assert.Len(t, sdk, 1)
}
