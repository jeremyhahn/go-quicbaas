//go:build rest

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

package rest

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/registry"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// The service registry REST surface (and, through the shared router, the
// QUIC transport, which is this REST dialect over HTTP/3):
//
//	POST   /api/v1/registry/instances                    Register
//	POST   /api/v1/registry/leases/{lease_id}/renew      Renew
//	DELETE /api/v1/registry/leases/{lease_id}            Deregister
//	GET    /api/v1/registry/services/{service}           Resolve
//	GET    /api/v1/registry/services/{service}/watch     Watch (SSE)
//
// Request and response bodies are the service-layer types, whose JSON tags
// are the wire contract; lease IDs travel as JSON strings so no client
// truncates them to a float.

const (
	// registrySSEHeartbeatInterval is how often an idle watch puts an SSE
	// comment on the wire, so a proxy never reaps a quiet stream and a
	// departed client is noticed without waiting for the next change.
	registrySSEHeartbeatInterval = 15 * time.Second

	// registrySSEEvent is the SSE event name of a registration change.
	registrySSEEvent = "registry"

	// RegistryWatchCompactedCode is the error code a watch reports when its
	// start revision has been compacted: as the HTTP 410 body at open time,
	// or as the `event: error` frame of a running stream. A client resolves
	// again and watches from the revision Resolve returns.
	RegistryWatchCompactedCode = "watch_compacted"

	// RegistryWatchEndedCode is the error code of a running stream the store
	// ended for any other reason.
	RegistryWatchEndedCode = "watch_ended"
)

// RegistryWatchEventREST is the data payload of one `event: registry` frame.
type RegistryWatchEventREST struct {
	// Type is "PUT" or "DELETE".
	Type string `json:"type"`

	// Instance is the affected instance.
	Instance service.RegistryServiceInstance `json:"instance"`

	// Revision is the store revision of the change.
	Revision int64 `json:"revision"`
}

// RegistryWatchErrorREST is the data payload of a terminal `event: error`
// frame, and the body of an HTTP 410 at open time.
type RegistryWatchErrorREST struct {
	// Code is RegistryWatchCompactedCode or RegistryWatchEndedCode.
	Code string `json:"code"`

	// Error describes the failure.
	Error string `json:"error"`
}

// HandleRegistryRegister handles POST /api/v1/registry/instances.
func (s *Server) HandleRegistryRegister(w http.ResponseWriter, r *http.Request) {
	reg, ok := s.registryService(w, r)
	if !ok {
		return
	}
	var req service.RegisterServiceInstanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}
	resp, err := reg.Register(r.Context(), &req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusCreated, resp)
}

// HandleRegistryRenew handles POST /api/v1/registry/leases/{lease_id}/renew.
func (s *Server) HandleRegistryRenew(w http.ResponseWriter, r *http.Request) {
	reg, ok := s.registryService(w, r)
	if !ok {
		return
	}
	leaseID, ok := registryLeaseParam(w, r)
	if !ok {
		return
	}
	resp, err := reg.Renew(r.Context(), &service.RenewServiceLeaseRequest{LeaseID: leaseID})
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleRegistryDeregister handles DELETE /api/v1/registry/leases/{lease_id}.
func (s *Server) HandleRegistryDeregister(w http.ResponseWriter, r *http.Request) {
	reg, ok := s.registryService(w, r)
	if !ok {
		return
	}
	leaseID, ok := registryLeaseParam(w, r)
	if !ok {
		return
	}
	if err := reg.Deregister(r.Context(), &service.DeregisterServiceInstanceRequest{LeaseID: leaseID}); err != nil {
		s.handleServiceError(w, r, err)
		return
	}
	RespondNoContent(w, r)
}

// HandleRegistryResolve handles GET /api/v1/registry/services/{service}.
func (s *Server) HandleRegistryResolve(w http.ResponseWriter, r *http.Request) {
	reg, ok := s.registryService(w, r)
	if !ok {
		return
	}
	resp, err := reg.Resolve(r.Context(), &service.ResolveServiceRequest{Service: chi.URLParam(r, "service")})
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleRegistryWatch handles GET
// /api/v1/registry/services/{service}/watch?from_revision=N as a Server-Sent
// Events stream. Each change is an `event: registry` frame whose `id:` is the
// change's revision; a store-ended watch closes with one `event: error`
// frame. A compacted start revision is refused before the stream opens with
// HTTP 410 Gone.
func (s *Server) HandleRegistryWatch(w http.ResponseWriter, r *http.Request) {
	reg, ok := s.registryService(w, r)
	if !ok {
		return
	}
	fromRevision := int64(0)
	if raw := r.URL.Query().Get("from_revision"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 0 {
			RespondBadRequest(w, r, "from_revision must be a non-negative integer")
			return
		}
		fromRevision = parsed
	}

	ctx := r.Context()
	events, err := reg.Watch(ctx, &service.WatchServiceRequest{
		Service:      chi.URLParam(r, "service"),
		FromRevision: fromRevision,
	})
	if err != nil {
		var compacted *registry.WatchCompactedError
		if stderrors.As(err, &compacted) {
			RespondJSON(w, r, http.StatusGone, &RegistryWatchErrorREST{Code: RegistryWatchCompactedCode, Error: compacted.Error()})
			return
		}
		s.handleServiceError(w, r, err)
		return
	}
	if err := serveRegistryWatchSSE(ctx, w, events, registrySSEHeartbeatInterval); err != nil && s.logger != nil {
		// The stream is already open, so the failure cannot become an HTTP
		// status; it is a departed client or a broken connection, reported
		// at debug level because it is routine.
		s.logger.WithField("component", "registry-watch").Debug(err.Error())
	}
}

// serveRegistryWatchSSE writes events as an SSE stream until ctx ends, the
// channel closes, or the store ends the watch. It follows the session-event
// stream's conventions (serveSessionEventsSSE): the per-connection write
// deadline is cleared, an unflushable writer is tolerated, and an idle stream
// is kept alive with comment frames. It returns the write failure that ended
// the stream, or nil when it ended normally.
func serveRegistryWatchSSE(ctx context.Context, w http.ResponseWriter, events <-chan service.RegistryEvent, heartbeatInterval time.Duration) error {
	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil && !stderrors.Is(err, http.ErrNotSupported) {
		return err
	}
	flush := func() error {
		if err := rc.Flush(); err != nil && !stderrors.Is(err, http.ErrNotSupported) {
			return err
		}
		return nil
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if err := flush(); err != nil {
		return err
	}

	heartbeat := time.NewTicker(heartbeatInterval)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-events:
			if !ok {
				return nil
			}
			if ev.Err != nil {
				return writeRegistryWatchError(w, flush, ev.Err)
			}
			if err := writeRegistryWatchEvent(w, flush, ev); err != nil {
				return err
			}
		case <-heartbeat.C:
			if _, err := fmt.Fprintf(w, ": ping %d\n\n", time.Now().Unix()); err != nil {
				return err
			}
			if err := flush(); err != nil {
				return err
			}
		}
	}
}

// writeRegistryWatchEvent writes one change as an SSE frame.
func writeRegistryWatchEvent(w http.ResponseWriter, flush func() error, ev service.RegistryEvent) error {
	payload, err := json.Marshal(&RegistryWatchEventREST{Type: ev.Type.String(), Instance: ev.Instance, Revision: ev.Revision})
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", ev.Revision, registrySSEEvent, payload); err != nil {
		return err
	}
	return flush()
}

// writeRegistryWatchError writes the terminal error frame of a store-ended
// watch and returns the write failure, if any.
func writeRegistryWatchError(w http.ResponseWriter, flush func() error, cause error) error {
	frame := &RegistryWatchErrorREST{Code: RegistryWatchEndedCode, Error: cause.Error()}
	var compacted *registry.WatchCompactedError
	if stderrors.As(cause, &compacted) {
		frame.Code = RegistryWatchCompactedCode
	}
	payload, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: error\ndata: %s\n\n", payload); err != nil {
		return err
	}
	return flush()
}

// registryService returns the configured service registry, answering 503
// itself when the server has none.
func (s *Server) registryService(w http.ResponseWriter, r *http.Request) (service.RegistryService, bool) {
	reg := s.service.Registry()
	if reg == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrRegistryServiceUnavailable)
		return nil, false
	}
	return reg, true
}

// registryLeaseParam parses the {lease_id} path parameter, answering 400
// itself when it is not a positive integer.
func registryLeaseParam(w http.ResponseWriter, r *http.Request) (int64, bool) {
	leaseID, err := strconv.ParseInt(chi.URLParam(r, "lease_id"), 10, 64)
	if err != nil || leaseID <= 0 {
		RespondError(w, r, http.StatusBadRequest, &qberrors.QBError{
			Code: qberrors.ErrInvalidInput,
			Op:   "rest.registry",
			Err:  &registry.InvalidInstanceError{Reason: "lease_id must be a positive integer"},
		})
		return 0, false
	}
	return leaseID, true
}
