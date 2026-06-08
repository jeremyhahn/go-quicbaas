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

package quicbaas

import (
	"context"
	"testing"

	dane "github.com/jeremyhahn/go-xkms/sdk/go/dane"
)

// recordingDANEPublisher implements the re-exported quicbaas.DANEPublisher
// alias. It exists to prove an embedding consumer can satisfy the interface
// using only the SDK surface (no direct go-xkms import is required at the
// consumer call site, though the alias resolves to the same xkms type).
type recordingDANEPublisher struct {
	records []dane.Record
}

func (p *recordingDANEPublisher) PublishDANE(_ context.Context, records []dane.Record) error {
	p.records = append(p.records, records...)
	return nil
}

// The DANEPublisher type alias must resolve to the xkms publisher interface so
// a consumer implementing it can be supplied to WithDANEPublisher.
func TestDANEPublisherAlias_IsXKMSPublisher(t *testing.T) {
	var _ DANEPublisher = (*recordingDANEPublisher)(nil)
	var _ dane.DANEPublisher = (*recordingDANEPublisher)(nil)
}

// WithDANEPublisher is the composer re-export of server.WithDANEPublisher. It
// must return a usable ServerOption and that option must be accepted by
// quicbaas.NewServer so an embedding consumer can register a DNS-backed DANE
// publisher at server construction time.
func TestWithDANEPublisherReExport_ReachableAndApplies(t *testing.T) {
	pub := &recordingDANEPublisher{}

	opt := WithDANEPublisher(pub)
	if opt == nil {
		t.Fatal("WithDANEPublisher returned a nil ServerOption")
	}

	cfg := TestServerConfig()
	srv, err := NewServer(cfg, opt)
	if err != nil {
		t.Fatalf("NewServer with WithDANEPublisher: %v", err)
	}
	if srv == nil {
		t.Fatal("NewServer returned a nil server")
	}
}

// NewServer must remain usable without a DANE publisher: the re-export is
// purely additive and a server constructed without it still builds.
func TestNewServer_WithoutDANEPublisher(t *testing.T) {
	srv, err := NewServer(TestServerConfig())
	if err != nil {
		t.Fatalf("NewServer without DANE publisher: %v", err)
	}
	if srv == nil {
		t.Fatal("NewServer returned a nil server")
	}
}
