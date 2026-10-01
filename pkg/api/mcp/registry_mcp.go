//go:build mcp

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
	"strconv"
	"time"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/registry"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

const (
	// registryWatchDefaultTimeout is how long quicbaas_registry_watch waits
	// for a change when the caller names no timeout.
	registryWatchDefaultTimeout = 5 * time.Second

	// registryWatchMaxTimeoutSeconds bounds one long poll, keeping a tool call
	// well inside every MCP client's request timeout.
	registryWatchMaxTimeoutSeconds = 30
)

// registryToolExecutor serves one registry tool.
type registryToolExecutor func(r *ToolRegistry, ctx context.Context, svc service.RegistryService, args map[string]interface{}) (interface{}, error)

// registryExecutors dispatches on the tool name in constant time.
var registryExecutors = map[string]registryToolExecutor{
	"quicbaas_registry_register":   (*ToolRegistry).handleRegistryRegister,
	"quicbaas_registry_renew":      (*ToolRegistry).handleRegistryRenew,
	"quicbaas_registry_deregister": (*ToolRegistry).handleRegistryDeregister,
	"quicbaas_registry_resolve":    (*ToolRegistry).handleRegistryResolve,
	"quicbaas_registry_watch":      (*ToolRegistry).handleRegistryWatch,
}

// registerRegistryTools registers the service registry tools.
func (r *ToolRegistry) registerRegistryTools() {
	for _, tool := range getAllRegistryTools() {
		r.addTool(ToolDefinition{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: convertInputSchema(tool.InputSchema),
		})
	}
}

// isRegistryTool reports whether the tool name is a service registry tool.
func isRegistryTool(name string) bool {
	_, ok := registryExecutors[name]
	return ok
}

// executeRegistryTool executes a service registry tool.
func (r *ToolRegistry) executeRegistryTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	svc := r.service.Registry()
	if svc == nil {
		return nil, api.NewServiceNotConfiguredError("registry")
	}
	executor, ok := registryExecutors[name]
	if !ok {
		return nil, api.NewUnknownToolError(name)
	}
	return executor(r, ctx, svc, args)
}

// handleRegistryRegister handles quicbaas_registry_register.
func (r *ToolRegistry) handleRegistryRegister(ctx context.Context, svc service.RegistryService, args map[string]interface{}) (interface{}, error) {
	for _, field := range []string{"service", "id", "address"} {
		if getString(args, field) == "" {
			return nil, api.NewRequiredFieldError(field)
		}
	}
	resp, err := svc.Register(ctx, &service.RegisterServiceInstanceRequest{
		Instance: service.RegistryServiceInstance{
			ID:       getString(args, "id"),
			Service:  getString(args, "service"),
			Address:  getString(args, "address"),
			Metadata: getStringMap(args, "metadata"),
		},
		TTLSeconds: getInt64(args, "ttl_seconds"),
	})
	if err != nil {
		return nil, api.NewOperationFailedError("registry register", err)
	}
	return map[string]interface{}{
		"lease_id":    strconv.FormatInt(resp.LeaseID, 10),
		"ttl_seconds": resp.TTLSeconds,
	}, nil
}

// handleRegistryRenew handles quicbaas_registry_renew.
func (r *ToolRegistry) handleRegistryRenew(ctx context.Context, svc service.RegistryService, args map[string]interface{}) (interface{}, error) {
	leaseID, err := registryLeaseArg(args)
	if err != nil {
		return nil, err
	}
	resp, err := svc.Renew(ctx, &service.RenewServiceLeaseRequest{LeaseID: leaseID})
	if err != nil {
		return nil, api.NewOperationFailedError("registry renew", err)
	}
	return map[string]interface{}{
		"lease_id":    strconv.FormatInt(resp.LeaseID, 10),
		"ttl_seconds": resp.TTLSeconds,
	}, nil
}

// handleRegistryDeregister handles quicbaas_registry_deregister.
func (r *ToolRegistry) handleRegistryDeregister(ctx context.Context, svc service.RegistryService, args map[string]interface{}) (interface{}, error) {
	leaseID, err := registryLeaseArg(args)
	if err != nil {
		return nil, err
	}
	if err := svc.Deregister(ctx, &service.DeregisterServiceInstanceRequest{LeaseID: leaseID}); err != nil {
		return nil, api.NewOperationFailedError("registry deregister", err)
	}
	return map[string]interface{}{
		"lease_id":     strconv.FormatInt(leaseID, 10),
		"deregistered": true,
	}, nil
}

// handleRegistryResolve handles quicbaas_registry_resolve.
func (r *ToolRegistry) handleRegistryResolve(ctx context.Context, svc service.RegistryService, args map[string]interface{}) (interface{}, error) {
	serviceName := getString(args, "service")
	if serviceName == "" {
		return nil, api.NewRequiredFieldError("service")
	}
	resp, err := svc.Resolve(ctx, &service.ResolveServiceRequest{Service: serviceName})
	if err != nil {
		return nil, api.NewOperationFailedError("registry resolve", err)
	}
	instances := make([]map[string]interface{}, 0, len(resp.Instances))
	for _, inst := range resp.Instances {
		instances = append(instances, registryInstanceResult(inst))
	}
	return map[string]interface{}{
		"instances": instances,
		"revision":  resp.Revision,
	}, nil
}

// handleRegistryWatch handles quicbaas_registry_watch: a bounded long poll.
//
// It waits for the first change at or after from_revision, then takes every
// change already queued behind it, and returns. The result's revision is the
// highest revision DELIVERED — never a revision merely observed — so a caller
// that continues from revision+1 neither skips nor repeats a change. A call
// that delivers nothing reports from_revision-1, which continues from the same
// place. The loss-free cursor contract mirrors go-qrdb's own qrdb_watch tool.
//
// A start revision older than the retained history is not a tool failure: the
// result says compacted=true so a client can resynchronise structurally. A
// watch the store ends mid-poll reports ended=true likewise.
func (r *ToolRegistry) handleRegistryWatch(ctx context.Context, svc service.RegistryService, args map[string]interface{}) (interface{}, error) {
	serviceName := getString(args, "service")
	if serviceName == "" {
		return nil, api.NewRequiredFieldError("service")
	}
	fromRevision := getInt64(args, "from_revision")
	if fromRevision < 0 {
		return nil, api.NewInvalidInputError("from_revision must not be negative")
	}
	timeout := registryWatchDefaultTimeout
	if seconds := getInt(args, "timeout_seconds"); seconds != 0 {
		if seconds < 1 || seconds > registryWatchMaxTimeoutSeconds {
			return nil, api.NewRangeError("timeout_seconds", 1, registryWatchMaxTimeoutSeconds)
		}
		timeout = time.Duration(seconds) * time.Second
	}

	pollCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cursor := fromRevision - 1
	if cursor < 0 {
		cursor = 0
	}
	result := map[string]interface{}{
		"events":    []map[string]interface{}{},
		"revision":  cursor,
		"compacted": false,
		"ended":     false,
	}

	events, err := svc.Watch(pollCtx, &service.WatchServiceRequest{Service: serviceName, FromRevision: fromRevision})
	if err != nil {
		var compacted *registry.WatchCompactedError
		if stderrors.As(err, &compacted) {
			result["compacted"] = true
			result["error"] = compacted.Error()
			return result, nil
		}
		return nil, api.NewOperationFailedError("registry watch", err)
	}

	collected := collectRegistryEvents(pollCtx, events)
	delivered := make([]map[string]interface{}, 0, len(collected.events))
	for _, ev := range collected.events {
		delivered = append(delivered, registryEventResult(ev))
		if ev.Revision > cursor {
			cursor = ev.Revision
		}
	}
	result["events"] = delivered
	result["revision"] = cursor
	if collected.err != nil {
		var compacted *registry.WatchCompactedError
		result["compacted"] = stderrors.As(collected.err, &compacted)
		result["ended"] = !stderrors.As(collected.err, &compacted)
		result["error"] = collected.err.Error()
	}
	return result, nil
}

// registryPoll is what one long poll collected.
type registryPoll struct {
	events []service.RegistryEvent
	err    error
}

// collectRegistryEvents waits for the first change (or the poll's deadline),
// then drains what is already queued without waiting again, so one call
// returns a change promptly and still batches a burst.
func collectRegistryEvents(ctx context.Context, events <-chan service.RegistryEvent) registryPoll {
	var poll registryPoll
	select {
	case <-ctx.Done():
		return poll
	case ev, ok := <-events:
		if !ok {
			return poll
		}
		if ev.Err != nil {
			poll.err = ev.Err
			return poll
		}
		poll.events = append(poll.events, ev)
	}
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return poll
			}
			if ev.Err != nil {
				poll.err = ev.Err
				return poll
			}
			poll.events = append(poll.events, ev)
		default:
			return poll
		}
	}
}

// registryLeaseArg parses the lease_id argument, a positive decimal string
// (an integer is accepted from callers that send one).
func registryLeaseArg(args map[string]interface{}) (int64, error) {
	raw, present := args["lease_id"]
	if !present || raw == nil || raw == "" {
		return 0, api.NewRequiredFieldError("lease_id")
	}
	var leaseID int64
	switch v := raw.(type) {
	case string:
		parsed, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return 0, api.NewInvalidFormatError("lease_id", err)
		}
		leaseID = parsed
	default:
		leaseID = getInt64(args, "lease_id")
	}
	if leaseID <= 0 {
		return 0, api.NewInvalidInputError("lease_id must be a positive integer")
	}
	return leaseID, nil
}

// registryInstanceResult renders an instance as a tool result object.
func registryInstanceResult(inst service.RegistryServiceInstance) map[string]interface{} {
	out := map[string]interface{}{
		"id":      inst.ID,
		"service": inst.Service,
		"address": inst.Address,
	}
	if len(inst.Metadata) > 0 {
		out["metadata"] = inst.Metadata
	}
	return out
}

// registryEventResult renders a change as a tool result object.
func registryEventResult(ev service.RegistryEvent) map[string]interface{} {
	return map[string]interface{}{
		"type":     ev.Type.String(),
		"instance": registryInstanceResult(ev.Instance),
		"revision": ev.Revision,
	}
}
