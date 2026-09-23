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

package server

import (
	"errors"
	"testing"

	secure "github.com/jeremyhahn/go-xkms/sdk/go/secure"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// TestClusterCAKeyProtection_SealsThePassphrase covers the conversion go-xkms
// requires: the operator passphrase reaches the cluster CA keystore as a
// sealed secret holding exactly the configured bytes, and an unset passphrase
// leaves passphrase mode off rather than sealing an empty one.
func TestClusterCAKeyProtection_SealsThePassphrase(t *testing.T) {
	cfg := config.Default()
	cfg.Raft.RaftEnrollment.CAKeyProtection = "passphrase"
	cfg.Raft.RaftEnrollment.CAKeyPassphrase = "cluster-ca-passphrase"
	s := &Server{config: cfg}

	protection, err := s.clusterCAKeyProtection()
	require.NoError(t, err)
	require.NotNil(t, protection.Passphrase)
	buf, err := protection.Passphrase.Open()
	require.NoError(t, err)
	defer buf.Destroy()
	assert.Equal(t, "cluster-ca-passphrase", string(buf.Bytes()))
	assert.False(t, protection.AllowInsecure)

	cfg.Raft.RaftEnrollment.CAKeyPassphrase = ""
	protection, err = s.clusterCAKeyProtection()
	require.NoError(t, err)
	assert.Nil(t, protection.Passphrase, "no passphrase configured: passphrase mode stays off")
}

// TestClusterCAKeyProtection_ProtectionFailureIsFatal covers the error path:
// protected memory the kernel refuses must fail the cluster CA, never run it
// with passphrase protection silently disabled.
func TestClusterCAKeyProtection_ProtectionFailureIsFatal(t *testing.T) {
	cfg := config.Default()
	cfg.Raft.RaftEnrollment.CAKeyProtection = "passphrase"
	cfg.Raft.RaftEnrollment.CAKeyPassphrase = "cluster-ca-passphrase"
	s := &Server{config: cfg}

	sentinel := errors.New("mlock refused: cannot allocate locked pages")
	restore := newClusterCAPassphrase
	newClusterCAPassphrase = func([]byte) (*secure.Secret, error) { return nil, sentinel }
	defer func() { newClusterCAPassphrase = restore }()

	_, err := s.clusterCAKeyProtection()
	var typed *ClusterCAPassphraseError
	require.ErrorAs(t, err, &typed)
	assert.ErrorIs(t, err, sentinel)

	deps, err := s.clusterStrategyDeps(nil, nil)
	assert.Nil(t, deps)
	assert.ErrorAs(t, err, &typed, "the strategy deps must not be built without the passphrase")
}
