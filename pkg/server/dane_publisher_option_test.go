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

package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dane "github.com/jeremyhahn/go-xkms/sdk/go/dane"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// fakeDANEPublisher is a no-op DANEPublisher used to verify the WithDANEPublisher
// option records the supplied publisher on the server options.
type fakeDANEPublisher struct{}

func (fakeDANEPublisher) PublishDANE(_ context.Context, _ []dane.Record) error { return nil }

// WithDANEPublisher records the application-supplied publisher on the server
// options so Security().Trust().PublishDANE can dispatch to it. Until one is
// registered the field stays nil and PublishDANE fails closed.
func TestWithDANEPublisher_StoredOnServerOptions(t *testing.T) {
	pub := fakeDANEPublisher{}
	opt := WithDANEPublisher(pub)
	assert.NotNil(t, opt)

	srv, err := NewServer(config.Default(), opt)
	require.NoError(t, err)
	assert.NotNil(t, srv.opts.danePublisher,
		"WithDANEPublisher must record the publisher on serverOptions")
}

// A server constructed without WithDANEPublisher leaves the publisher nil.
func TestWithDANEPublisher_DefaultNil(t *testing.T) {
	srv, err := NewServer(config.Default())
	require.NoError(t, err)
	assert.Nil(t, srv.opts.danePublisher,
		"danePublisher must be nil when WithDANEPublisher is not supplied")
}
