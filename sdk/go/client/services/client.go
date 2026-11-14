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

package services

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// Client is the service plane client: the seven lifecycle verbs over the
// caller tenant's signed services, and reads.
type Client interface {
	// Deploy verifies a signed manifest, grants the service its identity,
	// certificate, name, RBAC and secrets, and launches it.
	Deploy(ctx context.Context, manifest *SignedManifest) (*Record, error)

	// Redeploy moves a settled service to a newer signed manifest.
	Redeploy(ctx context.Context, manifest *SignedManifest) (*Record, error)

	// Undeploy halts a settled service and revokes its grants.
	Undeploy(ctx context.Context, name string) error

	// Start starts a stopped service.
	Start(ctx context.Context, name string) (*Record, error)

	// Stop stops a running service.
	Stop(ctx context.Context, name string) (*Record, error)

	// Resume resumes a service whose last verb failed.
	Resume(ctx context.Context, name string) (*Record, error)

	// Restart restarts a running or halted service.
	Restart(ctx context.Context, name string) (*Record, error)

	// Get reads one service.
	Get(ctx context.Context, name string) (*Record, error)

	// List lists the caller tenant's services.
	List(ctx context.Context) (*ListResponse, error)
}

type client struct{ t transport.Transport }

// NewClient creates a service plane client over a transport.
func NewClient(t transport.Transport) Client { return &client{t: t} }

func (c *client) Deploy(ctx context.Context, manifest *SignedManifest) (*Record, error) {
	return c.manifest(ctx, "Deploy", manifest)
}

func (c *client) Redeploy(ctx context.Context, manifest *SignedManifest) (*Record, error) {
	return c.manifest(ctx, "Redeploy", manifest)
}

func (c *client) Undeploy(ctx context.Context, name string) error {
	if name == "" {
		return errors.NewInvalidInput("services.Undeploy", "name is required")
	}
	_, err := c.t.Request(ctx, ServiceName, "Undeploy", &NameRequest{Name: name})
	return err
}

func (c *client) Start(ctx context.Context, name string) (*Record, error) {
	return c.named(ctx, "Start", name)
}

func (c *client) Stop(ctx context.Context, name string) (*Record, error) {
	return c.named(ctx, "Stop", name)
}

func (c *client) Resume(ctx context.Context, name string) (*Record, error) {
	return c.named(ctx, "Resume", name)
}

func (c *client) Restart(ctx context.Context, name string) (*Record, error) {
	return c.named(ctx, "Restart", name)
}

func (c *client) Get(ctx context.Context, name string) (*Record, error) {
	return c.named(ctx, "Get", name)
}

func (c *client) List(ctx context.Context) (*ListResponse, error) {
	resp, err := c.t.Request(ctx, ServiceName, "List", &ListRequest{})
	if err != nil {
		return nil, err
	}
	out, ok := resp.(*ListResponse)
	if !ok {
		return nil, errors.NewInternal("services.List", "invalid response type")
	}
	return out, nil
}

func (c *client) manifest(ctx context.Context, method string, manifest *SignedManifest) (*Record, error) {
	op := "services." + method
	if manifest == nil || len(manifest.Payload) == 0 || len(manifest.Signature) == 0 || manifest.KeyID == "" {
		return nil, errors.NewInvalidInput(op, "a signed manifest (payload, key_id, signature) is required")
	}
	return record(op)(c.t.Request(ctx, ServiceName, method, &DeployRequest{Manifest: manifest}))
}

func (c *client) named(ctx context.Context, method, name string) (*Record, error) {
	op := "services." + method
	if name == "" {
		return nil, errors.NewInvalidInput(op, "name is required")
	}
	return record(op)(c.t.Request(ctx, ServiceName, method, &NameRequest{Name: name}))
}

func record(op string) func(interface{}, error) (*Record, error) {
	return func(resp interface{}, err error) (*Record, error) {
		if err != nil {
			return nil, err
		}
		out, ok := resp.(*Record)
		if !ok {
			return nil, errors.NewInternal(op, "invalid response type")
		}
		return out, nil
	}
}
