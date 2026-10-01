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

package mcp

import (
	"context"
	stderrors "errors"
	"testing"
	"time"

	registryclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/registry"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

func TestRegistryMCP_Register(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_registry_register",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			if args["service"] != "api" || args["id"] != "i" || args["address"] != "10.0.0.9:443" || args["ttl_seconds"] != float64(15) {
				t.Errorf("server received %v", args)
			}
			if md, ok := args["metadata"].(map[string]interface{}); !ok || md["zone"] != "z" {
				t.Errorf("server received metadata %v", args["metadata"])
			}
		},
		result: map[string]interface{}{"lease_id": "9007199254740993", "ttl_seconds": 15},
	})
	defer cleanup()

	out, err := tr.Request(context.Background(), registryclient.ServiceName, "Register", &registryclient.RegisterRequest{
		Instance:   registryclient.ServiceInstance{ID: "i", Service: "api", Address: "10.0.0.9:443", Metadata: map[string]string{"zone": "z"}},
		TTLSeconds: 15,
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if lease := out.(*registryclient.RegisterResponse); lease.LeaseID != 9007199254740993 || lease.TTLSeconds != 15 {
		t.Fatalf("Register = %+v (a lease beyond 2^53 must survive)", lease)
	}
}

func TestRegistryMCP_RenewDeregisterResolve(t *testing.T) {
	ctx := context.Background()
	leaseArg := func(want string) func(*testing.T, map[string]interface{}) {
		return func(t *testing.T, args map[string]interface{}) {
			if args["lease_id"] != want {
				t.Errorf("server received lease_id %v, want the string %q", args["lease_id"], want)
			}
		}
	}

	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_registry_renew", assertArgs: leaseArg("77"),
		result: map[string]interface{}{"lease_id": "77", "ttl_seconds": 9},
	})
	out, err := tr.Request(ctx, registryclient.ServiceName, "Renew", &registryclient.RenewRequest{LeaseID: 77})
	cleanup()
	if err != nil || out.(*registryclient.RenewResponse).TTLSeconds != 9 {
		t.Fatalf("Renew = %+v, %v", out, err)
	}

	tr, cleanup = newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_registry_deregister", assertArgs: leaseArg("78"),
		result: map[string]interface{}{"lease_id": "78", "deregistered": true},
	})
	out, err = tr.Request(ctx, registryclient.ServiceName, "Deregister", &registryclient.DeregisterRequest{LeaseID: 78})
	cleanup()
	if err != nil || out != nil {
		t.Fatalf("Deregister = %+v, %v", out, err)
	}

	tr, cleanup = newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_registry_resolve",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			if args["service"] != "api" {
				t.Errorf("server received %v", args)
			}
		},
		result: map[string]interface{}{
			"instances": []interface{}{map[string]interface{}{"id": "a", "service": "api", "address": "x:1", "metadata": map[string]interface{}{"k": "v"}}},
			"revision":  31,
		},
	})
	defer cleanup()
	out, err = tr.Request(ctx, registryclient.ServiceName, "Resolve", &registryclient.ResolveRequest{Service: "api"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved := out.(*registryclient.ResolveResponse); resolved.Revision != 31 || len(resolved.Instances) != 1 || resolved.Instances[0].Metadata["k"] != "v" {
		t.Fatalf("Resolve = %+v", resolved)
	}
}

func TestRegistryMCP_ArgumentAndResultErrors(t *testing.T) {
	for _, method := range []string{"Register", "Renew", "Deregister", "Resolve"} {
		if _, err := registryArguments(method, "wrong"); !sdkerrors.IsInvalidInput(err) {
			t.Fatalf("%s wrong type = %v", method, err)
		}
	}
	if _, err := registryArguments("Quarantine", nil); sdkerrors.Code(err) != sdkerrors.CodeUnimplemented {
		t.Fatalf("unknown method = %v", err)
	}
	if _, err := registryResult("Resolve", map[string]interface{}{"revision": "not a number"}); sdkerrors.Code(err) != sdkerrors.CodeInternal {
		t.Fatalf("malformed result = %v", err)
	}
	if out, err := registryResult("Deregister", nil); out != nil || err != nil {
		t.Fatalf("Deregister result = %v, %v", out, err)
	}
}

// TestRegistryMCP_WatchPollsWithTheCursor drives the watch over the real MCP
// server: the poll names the service, the cursor and a bounded wait, and a
// compacted answer ends the stream with the typed error.
func TestRegistryMCP_WatchPollsWithTheCursor(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_registry_watch",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			if args["service"] != "api" || args["from_revision"] != float64(12) || args["timeout_seconds"] != float64(4) {
				t.Errorf("server received %v", args)
			}
		},
		result: map[string]interface{}{
			"events": []interface{}{map[string]interface{}{
				"type": "PUT", "revision": 12,
				"instance": map[string]interface{}{"id": "a", "service": "api", "address": "x:1"},
			}},
			"revision":  12,
			"compacted": true,
			"error":     "history gone",
		},
	})
	defer cleanup()

	stream, err := tr.RequestStream(context.Background(), registryclient.ServiceName, "Watch", &registryclient.WatchRequest{Service: "api", FromRevision: 12})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	msg, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv: %v", err)
	}
	if ev := msg.(*registryclient.Event); ev.Type != registryclient.EventPut || ev.Revision != 12 || ev.Instance.Address != "x:1" {
		t.Fatalf("event = %+v", ev)
	}
	_, err = stream.Recv()
	var compacted *registryclient.WatchCompactedError
	if !stderrors.As(err, &compacted) || compacted.Message != "history gone" {
		t.Fatalf("end = %v, want *WatchCompactedError", err)
	}
}

// TestRegistryWatchPollStream_CursorIsLossFree pins the cursor rule: the next
// poll starts one past the last DELIVERED revision, and an empty poll leaves
// the cursor where it was.
func TestRegistryWatchPollStream_CursorIsLossFree(t *testing.T) {
	var froms []int64
	answers := []*registryWatchPoll{
		{Events: []registryclient.Event{{Type: registryclient.EventPut, Revision: 5}, {Type: registryclient.EventPut, Revision: 7}}, Revision: 7},
		{Revision: 7},
		{Events: []registryclient.Event{{Type: registryclient.EventDelete, Revision: 9}}, Revision: 9, Ended: true, Error: "evicted"},
	}
	s := &registryWatchPollStream{
		ctx: context.Background(), service: "api", next: 5, seconds: 1,
		poll: func(_ context.Context, service string, from int64, seconds int) (*registryWatchPoll, error) {
			if service != "api" || seconds != 1 {
				t.Errorf("poll(%q, %d s)", service, seconds)
			}
			froms = append(froms, from)
			answer := answers[0]
			answers = answers[1:]
			return answer, nil
		},
	}
	for _, want := range []int64{5, 7, 9} {
		msg, err := s.Recv()
		if err != nil || msg.(*registryclient.Event).Revision != want {
			t.Fatalf("Recv = %+v, %v, want revision %d", msg, err, want)
		}
	}
	var ended *registryclient.WatchEndedError
	if _, err := s.Recv(); !stderrors.As(err, &ended) || ended.Message != "evicted" {
		t.Fatalf("end = %v, want *WatchEndedError", err)
	}
	if len(froms) != 3 || froms[0] != 5 || froms[1] != 8 || froms[2] != 8 {
		t.Fatalf("polled from %v, want [5 8 8]", froms)
	}
}

func TestRegistryWatchPollStream_FailuresAndContext(t *testing.T) {
	boom := sdkerrors.New(sdkerrors.CodeConnection, "net", "refused")
	s := &registryWatchPollStream{ctx: context.Background(), service: "api", next: 1,
		poll: func(context.Context, string, int64, int) (*registryWatchPoll, error) { return nil, boom }}
	if _, err := s.Recv(); err != boom {
		t.Fatalf("poll failure = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s = &registryWatchPollStream{ctx: ctx, service: "api", next: 1,
		poll: func(context.Context, string, int64, int) (*registryWatchPoll, error) {
			t.Error("polled after the context ended")
			return &registryWatchPoll{}, nil
		}}
	if _, err := s.Recv(); !stderrors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Recv = %v", err)
	}
	if err := s.Send(nil); sdkerrors.Code(err) != sdkerrors.CodeUnimplemented {
		t.Fatalf("Send = %v", err)
	}
	if s.CloseSend() != nil || s.Close() != nil {
		t.Fatal("CloseSend and Close are no-ops")
	}
}

func TestRegistryMCP_WatchOpenAndPollTimeout(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{wantTool: "quicbaas_registry_watch", result: map[string]interface{}{}})
	defer cleanup()
	if _, err := tr.RequestStream(context.Background(), registryclient.ServiceName, "Resolve", nil); sdkerrors.Code(err) != sdkerrors.CodeUnimplemented {
		t.Fatalf("non-stream method = %v", err)
	}
	if _, err := tr.RequestStream(context.Background(), registryclient.ServiceName, "Watch", "wrong"); !sdkerrors.IsInvalidInput(err) {
		t.Fatalf("wrong type = %v", err)
	}
	if _, err := tr.RequestStream(context.Background(), "quicbaas.KV", "Watch", nil); sdkerrors.Code(err) != sdkerrors.CodeUnimplemented {
		t.Fatalf("other services still have no MCP stream: %v", err)
	}

	cases := map[time.Duration]int{0: registryPollSeconds, time.Hour: registryPollSeconds, 5 * time.Second: 4, time.Second: 1}
	for timeout, want := range cases {
		if got := registryPollTimeout(&transport.Config{Timeout: timeout}); got != want {
			t.Errorf("registryPollTimeout(%s) = %d, want %d", timeout, got, want)
		}
	}
	if registryPollTimeout(nil) != registryPollSeconds {
		t.Error("a nil config polls for the default duration")
	}
}
