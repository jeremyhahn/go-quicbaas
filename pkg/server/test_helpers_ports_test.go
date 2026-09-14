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
	"net"
	"strconv"
	"sync"
	"testing"
)

// portDraws is how many three-port configs the distinctness test draws.
//
// It is a small number on purpose. The old allocator could only be caught
// statistically — it probed with a listener it then closed, so a duplicate
// needed the kernel to hand the same ephemeral port straight back, which took
// thousands of draws to observe, and the test compensated with ten thousand
// trials. The invariant is now structural: a drawn port stays locked host-wide
// until the drawing test finishes, so a duplicate is impossible rather than
// unlikely, and repetitions buy nothing. What they would cost is real — every
// draw holds its three ports for the length of this test.
const portDraws = 128

// TestGetFreePorts_DrawsDistinctPorts pins the invariant a test config depends
// on: the REST, gRPC and Raft ports it is built from must differ.
//
// They are the three ports the embedded qrdb node binds. Handing it the same
// number twice makes the second listener fail with "address already in use" and
// fails whatever test happened to draw it — a failure with no connection to the
// code under test, which is exactly how it went unexplained in CI run 10115.
//
// The assertion is across ALL draws, not merely within each one: every port
// drawn while an earlier one is still held must be new.
func TestGetFreePorts_DrawsDistinctPorts(t *testing.T) {
	seen := make(map[int]int, portDraws*3)
	for draw := range portDraws {
		ports := getFreePorts(t, 3)
		if len(ports) != 3 {
			t.Fatalf("draw %d returned %d ports, want 3", draw, len(ports))
		}
		for _, port := range ports {
			if previous, dup := seen[port]; dup {
				t.Fatalf("draw %d returned %v: port %d was already handed to draw %d; "+
					"a config built from these binds one port for two listeners",
					draw, ports, port, previous)
			}
			seen[port] = draw
		}
	}
}

// parallelDrawers is how many concurrent draws the collision test makes. Twelve
// keeps it fast while making the interleaving genuinely concurrent.
const parallelDrawers = 12

// TestGetFreePorts_ParallelDrawersNeverShareAPort is the go-quicbaas#706
// assertion, at the helper that carried the defect.
//
// The old helper drew each port by listening on 127.0.0.1:0 and CLOSING the
// listener before returning the number. From that close until the server bound
// it the port was genuinely free, so two of this package's ~75 parallel
// standalone-node boots could be handed the same one and the second to bind
// died with "address already in use" — reddening tests, and whole other lanes'
// gates, for reasons unrelated to their diff.
//
// Every drawer here holds its ports for the whole test: the reservation is
// released on test cleanup, not when getFreePorts returns. So two drawers
// receiving the same number is that defect and nothing else, and the assertion
// is deterministic rather than probabilistic — which it could not have been
// against the old helper, where a collision was merely unlikely.
func TestGetFreePorts_ParallelDrawersNeverShareAPort(t *testing.T) {
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		owner = make(map[int]int, parallelDrawers*3)
		fault string
	)

	for drawer := range parallelDrawers {
		wg.Add(1)
		go func() {
			// Deferred so a t.Fatalf inside getFreePorts unwinds this goroutine
			// without hanging the test on Wait.
			defer wg.Done()

			ports := getFreePorts(t, 3)

			mu.Lock()
			defer mu.Unlock()
			for _, port := range ports {
				if previous, clash := owner[port]; clash && fault == "" {
					fault = "port " + strconv.Itoa(port) + " was handed to drawer " +
						strconv.Itoa(previous) + " and drawer " + strconv.Itoa(drawer) +
						" at the same time"
				}
				owner[port] = drawer
			}
		}()
	}
	wg.Wait()

	if fault != "" {
		t.Fatal(fault)
	}
	if len(owner) != parallelDrawers*3 {
		t.Fatalf("%d concurrent drawers of 3 yielded %d distinct ports, want %d",
			parallelDrawers, len(owner), parallelDrawers*3)
	}
}

// TestGetFreePorts_ReturnsBindablePorts covers the single- and multi-port
// shapes and pins what a caller actually needs from the number: that the server
// it is handed to can bind it, on TCP and on the UDP sibling an HTTP/3 listener
// takes at the same number.
func TestGetFreePorts_ReturnsBindablePorts(t *testing.T) {
	for _, count := range []int{1, 3, 5} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			ports := getFreePorts(t, count)
			if len(ports) != count {
				t.Fatalf("getFreePorts(%d) returned %d ports", count, len(ports))
			}
			for _, port := range ports {
				addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))

				listener, err := net.Listen("tcp", addr)
				if err != nil {
					t.Fatalf("reserved port %d is not bindable on TCP: %v", port, err)
				}
				packet, err := net.ListenPacket("udp", addr)
				if err != nil {
					t.Fatalf("reserved port %d is not bindable on UDP: %v", port, err)
				}
				if err := listener.Close(); err != nil {
					t.Fatalf("closing the TCP probe on %d: %v", port, err)
				}
				if err := packet.Close(); err != nil {
					t.Fatalf("closing the UDP probe on %d: %v", port, err)
				}
			}
		})
	}

	if port := getFreePort(t); port <= 0 {
		t.Fatalf("getFreePort = %d, want a bindable port", port)
	}
}
