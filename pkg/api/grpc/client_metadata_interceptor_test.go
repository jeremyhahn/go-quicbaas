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

package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
)

func TestConfiguredMetadata_AddsMissingKey(t *testing.T) {
	opts := defaultClientOptions()
	require.NoError(t, WithMetadata("authorization", "Bearer t")(opts))

	ctx := opts.withConfiguredMetadata(context.Background())

	md, ok := metadata.FromOutgoingContext(ctx)
	require.True(t, ok)
	assert.Equal(t, []string{"Bearer t"}, md.Get("authorization"))
}

func TestConfiguredMetadata_CallerValueWins(t *testing.T) {
	opts := defaultClientOptions()
	require.NoError(t, WithMetadata("authorization", "Bearer t")(opts))

	start := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer caller")
	ctx := opts.withConfiguredMetadata(start)

	md, ok := metadata.FromOutgoingContext(ctx)
	require.True(t, ok)
	assert.Equal(t, []string{"Bearer caller"}, md.Get("authorization"))
}

func TestMetadataDialOptions(t *testing.T) {
	assert.Len(t, defaultClientOptions().metadataDialOptions(), 0)

	opts := defaultClientOptions()
	require.NoError(t, WithMetadata("authorization", "Bearer t")(opts))
	assert.Len(t, opts.metadataDialOptions(), 2)
}
