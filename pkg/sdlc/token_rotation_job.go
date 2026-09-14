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
	"encoding/json"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
)

// RotationHandlerName is the scheduler handler name a token-rotation job names.
const RotationHandlerName = "sdlc.token_rotation"

// SchedulerHandlerRegistry is the slice of the scheduler this job needs.
//
// A port rather than the engine itself, so registration is testable without
// standing up a scheduler: *scheduler.SchedulerEngine satisfies it as it
// stands.
type SchedulerHandlerRegistry interface {
	RegisterHandler(name string, handler scheduler.JobHandler) error
}

// RegisterTokenRotationHandler wires rotation into a scheduler.
//
// Each firing RECOVERS BEFORE IT ROTATES. A scheduled run is the natural place
// to clean up after a previous one that died mid-rotation: doing it here means
// an orphaned credential lives until the next interval rather than until
// somebody notices, and it costs one read when there is nothing to recover.
func RegisterTokenRotationHandler(registry SchedulerHandlerRegistry, rotator *TokenRotator) error {
	if registry == nil {
		return &InvalidInputError{Field: "registry", Message: "must not be nil"}
	}
	if rotator == nil {
		return &InvalidInputError{Field: "rotator", Message: "must not be nil"}
	}

	return registry.RegisterHandler(RotationHandlerName,
		func(ctx context.Context, job *scheduler.Job) (*scheduler.JobResult, error) {
			var spec TokenRotationSpec
			if err := json.Unmarshal(job.Payload, &spec); err != nil {
				return nil, &InvalidInputError{
					Field:   "payload",
					Message: "is not a token rotation spec",
				}
			}

			if err := rotator.RecoverOrphan(ctx, spec); err != nil {
				return nil, err
			}

			result, err := rotator.Rotate(ctx, spec)
			if err != nil {
				return nil, err
			}

			// NAMES ONLY. The result is stored, logged and rendered to
			// operators; the credential this job just minted must not travel
			// with it.
			return &scheduler.JobResult{
				Success: true,
				Message: "rotated " + spec.Username + " to " + result.NewTokenName,
			}, nil
		})
}
