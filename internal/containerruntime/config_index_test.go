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

package containerruntime

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A job can carry more than ten mounts or ports; a refusal must name the
// entry it refused by its real index, not by the character '0'+i.
func TestContainerConfigValidate_NamesTheEntryAtAnyIndex(t *testing.T) {
	mounts := make([]Mount, 12)
	for i := range mounts {
		mounts[i] = Mount{Source: "/src", Target: "/dst", Type: "bind"}
	}
	mounts[11].Target = ""

	ports := make([]PortMapping, 12)
	for i := range ports {
		ports[i] = PortMapping{ContainerPort: 80, Protocol: "tcp"}
	}
	ports[10].ContainerPort = 0

	for name, tc := range map[string]struct {
		config *ContainerConfig
		field  string
	}{
		"mount": {&ContainerConfig{Name: "c", Image: "i", Mounts: mounts}, "mounts[11]"},
		"port":  {&ContainerConfig{Name: "c", Image: "i", Ports: ports}, "ports[10]"},
	} {
		t.Run(name, func(t *testing.T) {
			var invalid *ConfigValidationError
			require.True(t, errors.As(tc.config.Validate(), &invalid))
			assert.Equal(t, tc.field, invalid.Field)
		})
	}
}
