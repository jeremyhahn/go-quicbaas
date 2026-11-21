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

package embedded

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	servercaps "github.com/jeremyhahn/go-quicbaas/pkg/capabilities"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/capabilities"
)

// TestTransport_CapabilitiesGet reads the report in process through the SDK
// client and checks it is the one capabilities.Build makes from the service.
func TestTransport_CapabilitiesGet(t *testing.T) {
	mock := &mockQuicBaaS{}
	tr, err := New(mock)
	require.NoError(t, err)

	report, err := capabilities.NewClient(tr).Get(context.Background())
	require.NoError(t, err)

	want := servercaps.Build(mock, servercaps.Probes{TimeSeriesServed: true})
	require.Len(t, report.Subsystems, len(want.Subsystems))
	for name, sub := range want.Subsystems {
		got := report.Subsystems[name]
		assert.Equal(t, sub.Served, got.Served, name)
		assert.Equal(t, sub.Configured, got.Configured, name)
		assert.Equal(t, sub.Reason, got.Reason, name)
	}
	assert.Equal(t, want.Families, report.Families)

	// The embedded transport runs no transport server, so it has no CA probe.
	xkms := report.Subsystems[servercaps.SubsystemXKMSCA]
	assert.False(t, xkms.Configured)
	assert.NotEmpty(t, xkms.Reason)

	// The embedded transport always compiles its time series handlers; the
	// mock facade wires no time series storage.
	ts := report.Subsystems[servercaps.SubsystemTimeSeries]
	assert.True(t, ts.Served)
	assert.False(t, ts.Configured)
	assert.Equal(t, servercaps.ReasonNoTimeSeriesStorage, ts.Reason)

	// The embedded transport has no RAG handlers.
	assert.NotContains(t, report.Families, servercaps.FamilyRAG)
	// Nor any object transfer handlers.
	assert.NotContains(t, report.Families, servercaps.FamilyObjectTransfer)
}

// TestTransport_CapabilitiesFamiliesHaveEmbeddedHandlers checks that every
// family the embedded report advertises has at least one handler in the
// embedded dispatch maps, so a client of an advertised family never gets an
// unknown-method answer.
func TestTransport_CapabilitiesFamiliesHaveEmbeddedHandlers(t *testing.T) {
	tr, err := New(&mockQuicBaaS{})
	require.NoError(t, err)

	report, err := capabilities.NewClient(tr).Get(context.Background())
	require.NoError(t, err)

	// familyService maps each family to its SDK service name. A name ending
	// in "." is a prefix that spans several services.
	familyService := map[string]string{
		"account":            "quicbaas.Account",
		"acme":               "quicbaas.ACME",
		"agents":             "quicbaas.Agents",
		"attestation":        "quicbaas.Attestation",
		"auth":               "quicbaas.Auth",
		"backup":             "quicbaas.Backup",
		"batch":              "quicbaas.Batch",
		"cache":              "quicbaas.Cache",
		"certificate":        "quicbaas.Certificate",
		"cluster":            "quicbaas.Cluster",
		"crypto":             "quicbaas.Crypto",
		"dek":                "quicbaas.DEK",
		"dynamicsecrets":     "quicbaas.DynamicSecrets",
		"embedding":          "quicbaas.Embedding",
		"health":             "quicbaas.Health",
		"iam":                "quicbaas.IAM.",
		"kms":                "quicbaas.KMS",
		"kv":                 "quicbaas.KV",
		"objectstorage":      "quicbaas.ObjectStorage",
		"objectstorageadmin": "quicbaas.ObjectStorageAdmin",
		"objecttransfer":     "quicbaas.ObjectTransfer",
		"ollama":             "quicbaas.Ollama",
		"operations":         "quicbaas.Operations",
		"orchestrator":       "quicbaas.Orchestrator",
		"parameterstore":     "quicbaas.ParameterStore",
		"project":            "quicbaas.Project",
		"prompt":             "quicbaas.Prompts",
		"query":              "quicbaas.Query",
		"rbac":               "quicbaas.RBAC",
		"registry":           "quicbaas.Registry",
		"sdlc":               "quicbaas.SDLC",
		"secrets":            "quicbaas.Secrets",
		"ssh":                "quicbaas.SSH",
		"transaction":        "quicbaas.Transaction",
		"trust":              "quicbaas.Trust",
		"messaging":          "quicbaas.Messaging",
		"timeseries":         "quicbaas.TimeSeries",
		"vector":             "quicbaas.Vector",
		"scheduler":          "quicbaas.Scheduler",
	}
	// sdk/go/client/spend holds only types and has no service to dispatch.
	typesOnly := map[string]bool{"spend": true}

	hasPrefix := func(prefix string) bool {
		for key := range unaryHandlers {
			if strings.HasPrefix(key, prefix) {
				return true
			}
		}
		for key := range streamHandlers {
			if strings.HasPrefix(key, prefix) {
				return true
			}
		}
		return false
	}

	for _, family := range report.Families {
		if typesOnly[family] {
			continue
		}
		service, ok := familyService[family]
		if !ok {
			t.Errorf("family %q has no service name: add the family's service name to the table", family)
			continue
		}
		prefix := service
		if !strings.HasSuffix(prefix, ".") {
			prefix += "/"
		}
		assert.Truef(t, hasPrefix(prefix), "family %q is advertised but the embedded transport has no %s handler", family, prefix)
	}
}
