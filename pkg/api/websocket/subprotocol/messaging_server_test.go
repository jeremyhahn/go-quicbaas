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

package subprotocol

import (
	"crypto/fips140"
	"reflect"
	"sort"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

func TestNewMessagingServer_RegistersThreeSubprotocols(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:  "debug",
		Format: "console",
		Output: "stdout",
	})
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	srv, err := NewMessagingServer(log)
	if fips140.Enforced() {
		if err == nil {
			t.Fatal("NewMessagingServer succeeded under FIPS-only mode")
		}
		t.Skip("FIPS-only mode refuses the WebSocket server")
	}
	if err != nil {
		t.Fatalf("NewMessagingServer: %v", err)
	}

	got := srv.GetSubprotocols()
	sort.Strings(got)
	want := []string{"amqp", "mqtt", "stomp"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GetSubprotocols() = %v, want %v", got, want)
	}
	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
