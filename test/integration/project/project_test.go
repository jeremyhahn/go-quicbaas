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

//go:build integration

// Package project checks the project service as the production server
// composes it. The project CRUD cases run in sdk/go/transport/embedded over a
// hand-wired harness; this package keeps the one property that harness
// cannot show: that server Start wires the project service to the server's
// fleet registry, so a host affinity is verified against the real fleet.
package project

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	quicbaaserrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/server"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	projectSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/project"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// newProjectClient starts a real embedded server through its production
// constructor and returns an embedded SDK client bound to it.
func newProjectClient(t *testing.T) quicbaas.Client {
	t.Helper()
	dataDir := t.TempDir()

	cfg := config.EmbeddedConfig()
	cfg.IAM.SPIFFETrustDomain = helpers.SPIFFETrustDomain
	cfg.Storage.DataDir = filepath.Join(dataDir, "data")
	cfg.Storage.WALDir = filepath.Join(dataDir, "wal")
	cfg.Raft.NodeID = 1
	cfg.Logging.Level = "error"
	cfg.Server.Embedded = true

	srv, err := server.NewServer(cfg)
	require.NoError(t, err)
	require.NoError(t, srv.Start())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		assert.NoError(t, srv.Shutdown(ctx))
	})

	svc := srv.Service()
	require.NotNil(t, svc, "a started server must expose its service")
	client, err := quicbaas.NewClient("embedded://local", quicbaas.WithEmbedded(svc),
		quicbaas.WithEmbeddedIdentity(quicbaas.ServicePrincipalIdentity(srv)))
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, client.Close()) })
	return client
}

// A host affinity naming no node in the server's fleet registry is refused
// as a dangling reference, and nothing is stored. Were Start not to wire the
// fleet registry into the project service, the reference could not be
// checked at all and the refusal would be a different error.
func TestProjectLifecycle_RefusesADanglingReference(t *testing.T) {
	client := newProjectClient(t)
	ctx := context.Background()
	const tenantID = "tenant-refs"

	_, err := client.Project().Create(ctx, &quicbaas.CreateProjectRequest{
		TenantID:     tenantID,
		Name:         "proj-refs",
		HostAffinity: "no-such-node",
	})
	require.Error(t, err, "a host affinity naming no fleet node must be refused")
	var notFound *quicbaaserrors.ReferenceNotFoundError
	require.True(t, errors.As(err, &notFound), "want *ReferenceNotFoundError, got %T: %v", err, err)
	assert.Equal(t, "host_affinity", notFound.Field)

	listed, err := client.Project().List(ctx, &projectSDK.ListProjectsRequest{TenantID: tenantID})
	require.NoError(t, err)
	assert.Empty(t, listed.Projects, "a refused create must store nothing")
}
