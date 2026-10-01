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
	"encoding/json"
	"strconv"
	"strings"
	"time"

	registryclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/registry"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// The service registry over MCP. The unary verbs are the
// quicbaas_registry_{register,renew,deregister,resolve} tools; the watch is the
// quicbaas_registry_watch long poll, which this transport turns into a
// transport.Stream by polling from a revision cursor. Lease IDs cross MCP as
// decimal strings so no JSON number decoder rounds them.

const (
	// registryWatchTool is the long-poll tool behind Watch.
	registryWatchTool = "quicbaas_registry_watch"

	// registryPollSeconds is the longest a single poll waits, kept inside
	// the server's 30-second cap.
	registryPollSeconds = 25
)

// isMCPRegistryService reports whether the service name targets the service
// registry.
func isMCPRegistryService(service string) bool {
	return strings.HasPrefix(service, registryclient.ServiceName)
}

// registryRequestTransforms build each unary verb's tool arguments.
var registryRequestTransforms = map[string]func(interface{}) (map[string]interface{}, error){
	"Register": func(req interface{}) (map[string]interface{}, error) {
		r, ok := req.(*registryclient.RegisterRequest)
		if !ok || r == nil {
			return nil, errors.NewInvalidInput("mcp.registry.Register", "invalid request type, expected *registry.RegisterRequest")
		}
		args := map[string]interface{}{
			"service":     r.Instance.Service,
			"id":          r.Instance.ID,
			"address":     r.Instance.Address,
			"ttl_seconds": r.TTLSeconds,
		}
		if len(r.Instance.Metadata) > 0 {
			args["metadata"] = r.Instance.Metadata
		}
		return args, nil
	},
	"Renew": func(req interface{}) (map[string]interface{}, error) {
		r, ok := req.(*registryclient.RenewRequest)
		if !ok || r == nil {
			return nil, errors.NewInvalidInput("mcp.registry.Renew", "invalid request type, expected *registry.RenewRequest")
		}
		return map[string]interface{}{"lease_id": strconv.FormatInt(r.LeaseID, 10)}, nil
	},
	"Deregister": func(req interface{}) (map[string]interface{}, error) {
		r, ok := req.(*registryclient.DeregisterRequest)
		if !ok || r == nil {
			return nil, errors.NewInvalidInput("mcp.registry.Deregister", "invalid request type, expected *registry.DeregisterRequest")
		}
		return map[string]interface{}{"lease_id": strconv.FormatInt(r.LeaseID, 10)}, nil
	},
	"Resolve": func(req interface{}) (map[string]interface{}, error) {
		r, ok := req.(*registryclient.ResolveRequest)
		if !ok || r == nil {
			return nil, errors.NewInvalidInput("mcp.registry.Resolve", "invalid request type, expected *registry.ResolveRequest")
		}
		return map[string]interface{}{"service": r.Service}, nil
	},
}

// registryResponseTypes allocate each unary verb's typed result. Deregister
// has none.
var registryResponseTypes = map[string]func() interface{}{
	"Register": func() interface{} { return &registryclient.RegisterResponse{} },
	"Renew":    func() interface{} { return &registryclient.RenewResponse{} },
	"Resolve":  func() interface{} { return &registryclient.ResolveResponse{} },
}

// registryArguments converts a unary registry request into tool arguments.
func registryArguments(method string, req interface{}) (map[string]interface{}, error) {
	transform, ok := registryRequestTransforms[method]
	if !ok {
		return nil, errors.NewUnimplemented("mcp.registry."+method, "method not exposed by the registry MCP tool set")
	}
	return transform(req)
}

// registryResult decodes a unary tool result into the SDK response type.
func registryResult(method string, result interface{}) (interface{}, error) {
	newResponse, ok := registryResponseTypes[method]
	if !ok {
		return nil, nil
	}
	out := newResponse()
	if err := remarshal(result, out); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.registry."+method, "failed to decode tool result")
	}
	return out, nil
}

// remarshal decodes a JSON-shaped value (the decoded tool result) into a
// typed struct by its JSON tags.
func remarshal(in interface{}, out interface{}) error {
	data, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

// registryWatchPoll is the quicbaas_registry_watch result.
type registryWatchPoll struct {
	Events    []registryclient.Event `json:"events"`
	Revision  int64                  `json:"revision"`
	Compacted bool                   `json:"compacted"`
	Ended     bool                   `json:"ended"`
	Error     string                 `json:"error"`
}

// openRegistryWatch turns the long-poll tool into a stream. The registry
// client always opens a watch from a concrete revision, which is what makes
// the poll cursor loss-free.
func (t *Transport) openRegistryWatch(ctx context.Context, method string, req interface{}) (transport.Stream, error) {
	if method != "Watch" {
		return nil, errors.NewUnimplemented("mcp.registry."+method, "not a registry streaming method")
	}
	r, ok := req.(*registryclient.WatchRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("mcp.registry.Watch", "invalid request type, expected *registry.WatchRequest")
	}
	if t.closed.Load() {
		return nil, errors.NewConnection("mcp.registry.Watch", "transport is closed")
	}
	return &registryWatchPollStream{
		poll:    t.registryWatchPoll,
		ctx:     ctx,
		service: r.Service,
		next:    r.FromRevision,
		seconds: registryPollTimeout(t.cfg),
	}, nil
}

// registryPollTimeout keeps one poll inside the transport's request timeout,
// so a poll that waits its full duration never trips the HTTP deadline.
func registryPollTimeout(cfg *transport.Config) int {
	if cfg == nil || cfg.Timeout <= 0 {
		return registryPollSeconds
	}
	seconds := int(cfg.Timeout/time.Second) - 1
	if seconds < 1 {
		return 1
	}
	if seconds > registryPollSeconds {
		return registryPollSeconds
	}
	return seconds
}

// registryWatchPoll performs one long poll.
func (t *Transport) registryWatchPoll(ctx context.Context, service string, from int64, seconds int) (*registryWatchPoll, error) {
	if err := t.ensureInitialized(ctx); err != nil {
		return nil, err
	}
	if err := t.ensureAuthenticated(ctx); err != nil {
		return nil, err
	}
	result, err := t.callTool(ctx, registryWatchTool, map[string]interface{}{
		"service":         service,
		"from_revision":   from,
		"timeout_seconds": seconds,
	})
	if err != nil {
		return nil, err
	}
	var poll registryWatchPoll
	if err := remarshal(result, &poll); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.registry.Watch", "failed to decode watch poll")
	}
	return &poll, nil
}

// registryWatchPollStream presents successive long polls as one stream.
type registryWatchPollStream struct {
	poll    func(ctx context.Context, service string, from int64, seconds int) (*registryWatchPoll, error)
	ctx     context.Context
	service string
	seconds int

	// next is the revision the next poll starts from: one past the last
	// change delivered.
	next int64

	// pending holds the changes of the last poll not yet handed out, and
	// final the failure that ends the stream once they have been.
	pending []registryclient.Event
	final   error
}

// Send is not supported: the watch is server-to-client only.
func (s *registryWatchPollStream) Send(interface{}) error {
	return errors.NewUnimplemented("mcp.registry.Watch", "the watch stream does not accept client messages")
}

// CloseSend is a no-op: there is no client-to-server half.
func (s *registryWatchPollStream) CloseSend() error { return nil }

// Close is a no-op: each poll is a self-contained tool call, and an
// in-flight poll ends with the watch's context.
func (s *registryWatchPollStream) Close() error { return nil }

// Recv returns the next change, polling until one arrives, the server ends
// the watch, or the watch's context ends.
func (s *registryWatchPollStream) Recv() (interface{}, error) {
	for {
		if len(s.pending) > 0 {
			ev := s.pending[0]
			s.pending = s.pending[1:]
			return &ev, nil
		}
		if s.final != nil {
			return nil, s.final
		}
		if err := s.ctx.Err(); err != nil {
			return nil, err
		}
		poll, err := s.poll(s.ctx, s.service, s.next, s.seconds)
		if err != nil {
			return nil, err
		}
		if poll.Compacted {
			s.final = &registryclient.WatchCompactedError{Service: s.service, FromRevision: s.next, Message: poll.Error}
		} else if poll.Ended {
			s.final = &registryclient.WatchEndedError{Service: s.service, Message: poll.Error}
		}
		if len(poll.Events) > 0 && poll.Revision >= s.next {
			s.next = poll.Revision + 1
		}
		s.pending = poll.Events
	}
}
