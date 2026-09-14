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

package sdlc

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Ports — what the module needs from its host
// =============================================================================
//
// The module holds no store of its own beyond Store, no secrets, no git and
// no fleet. Each seam implementation names the narrow interface it needs;
// the host (pkg/service, pkg/server) supplies the implementations at
// registration time. Every interface here is small so a test can fake it.

// BindingReader reads project bindings.
type BindingReader interface {
	GetBinding(ctx context.Context, tenantID, projectID string) (*ProjectBinding, error)
	ListBindings(ctx context.Context, tenantID string) ([]*ProjectBinding, error)
}

// DeliveryStore reads and writes deliveries.
type DeliveryStore interface {
	GetDelivery(ctx context.Context, tenantID, taskID string) (*Delivery, error)
	PutDelivery(ctx context.Context, tenantID string, delivery *Delivery) error
	UpdateDelivery(ctx context.Context, tenantID string, delivery *Delivery) error
}

// ForgeResolver builds the forge adapter for a binding, resolving the token
// the binding's TokenSecretRef names in the tenant's secrets.
type ForgeResolver interface {
	ForgeFor(ctx context.Context, tenantID string, cfg ForgeConfig) (ForgeBackend, error)
}

// ProjectReader reads orchestration Projects; orchestration.ProjectStore
// satisfies it.
type ProjectReader interface {
	Get(ctx context.Context, tenantID, projectID string) (*orchestration.Project, error)
}

// TaskReader reads Tasks and publishes their artifacts;
// orchestration.AgentService satisfies it.
type TaskReader interface {
	GetTask(ctx context.Context, tenantID, taskID string) (*orchestration.Task, error)
	SubmitTaskArtifacts(ctx context.Context, tenantID, taskID string, artifacts []orchestration.Artifact) error
}

// ProjectNudger asks the engine's task-source poller to sweep one project
// now. A verified webhook delivery nudges rather than reconciles, so the
// poller stays the single path an issue becomes a Task through.
type ProjectNudger interface {
	NudgeProject(ctx context.Context, tenantID, projectID string) error
}

// ForgeTokenReader resolves the forge API token a binding names, as bytes.
//
// The runner needs the VALUE, not an adapter holding it: the token is written
// into the per-spawn workspace directory the worker's container mounts
// read-only, so the container can clone and push as the project's agent
// identity without the value ever appearing in argv or in the environment.
type ForgeTokenReader interface {
	ForgeToken(ctx context.Context, tenantID string, cfg ForgeConfig) ([]byte, error)
}
