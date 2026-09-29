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

package unix

import (
	"context"
	"testing"
	"time"
)

// TestTransport_WorkerImageVerbs_Delegate proves the Unix transport hands the
// worker image verbs to the gRPC transport rather than refusing them before
// the socket: with nothing listening, the only acceptable failure is the dial.
func TestTransport_WorkerImageVerbs_Delegate(t *testing.T) {
	for _, method := range []string{"WorkerImageStatus", "RefreshWorkerImage"} {
		t.Run(method, func(t *testing.T) {
			tr := newWiredTransport(t)
			defer func() {
				if err := tr.Close(); err != nil {
					t.Logf("close: %v", err)
				}
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()

			_, err := tr.Request(ctx, "quicbaas.SDLC", method, nil)
			if err == nil {
				t.Fatal("expected a connection error: no server is listening")
			}
			if refusedAsUnsupported(err) {
				t.Fatalf("%s was refused before the socket, dispatch regression: %v", method, err)
			}
		})
	}
}
