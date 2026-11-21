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

package service

import (
	"context"
	stderrors "errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
	schedulerhandlers "github.com/jeremyhahn/go-quicbaas/pkg/scheduler/handlers"
	"github.com/jeremyhahn/go-quicbaas/pkg/secrets"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// The metadata keys SetRotationPolicy writes and the rotation scan reads.
const (
	secretRotationEnabledKey  = "rotation_enabled"
	secretRotationIntervalKey = "rotation_interval"
	secretRotationNextKey     = "rotation_next"
	secretRotationLastKey     = "rotation_last"
	secretRotationReasonKey   = "rotation_reason"
	secretRotationScheduled   = "scheduled"
	secretRotationRetainKey   = "rotation_retain_versions"
)

// defaultRotationRetainVersions is how many versions a scheduled rotation
// keeps besides the current one when the policy names no retention.
const defaultRotationRetainVersions = 9

// rotationKeep is how many versions a rotated secret keeps: the current
// version plus the policy's rotation_retain_versions older ones.
func rotationKeep(metadata map[string]string) int {
	retain, err := strconv.Atoi(metadata[secretRotationRetainKey])
	if err != nil || retain <= 0 {
		retain = defaultRotationRetainVersions
	}
	return retain + 1
}

// secretsVersionPruner is the Manager surface a rotation uses to bound the
// versions each scheduled rotation adds.
type secretsVersionPruner interface {
	PruneVersions(ctx context.Context, path string, keep int) (int, error)
}

// SecretsRotationScanError is returned when the secrets rotation scan cannot
// list the secrets it has to check.
type SecretsRotationScanError struct {
	Cause error
}

func (e *SecretsRotationScanError) Error() string {
	return fmt.Sprintf("secrets rotation scan: list secrets: %v", e.Cause)
}

func (e *SecretsRotationScanError) Unwrap() error { return e.Cause }

// SecretRotationIntervalError is the failure of a due rotation policy whose
// interval cannot schedule a next rotation.
type SecretRotationIntervalError struct {
	Path     string
	Interval string
}

func (e *SecretRotationIntervalError) Error() string {
	return fmt.Sprintf("secret %s: rotation interval %q is not a positive duration", e.Path, e.Interval)
}

// registerSecretsRotation registers the secrets rotation handler and its
// system job, so a cluster performs each due rotation exactly once.
func registerSecretsRotation(engine *scheduler.SchedulerEngine, s *secretsService, interval time.Duration) error {
	job, err := schedulerhandlers.SecretsRotationJob(interval)
	if err != nil {
		return err
	}
	handler := schedulerhandlers.NewSecretsRotationHandler(s)
	if err := engine.RegisterHandler(handler.Name(), handler.Handler()); err != nil {
		return err
	}
	return engine.RegisterSystemJob(job)
}

// RotateDueSecrets performs every rotation policy whose next rotation has
// passed, across all tenants. A performed rotation writes the secret's
// current value as a new version (encrypted afresh by the backend), records
// rotation_reason=scheduled and rotation_last, and advances rotation_next by
// the policy's interval. A secret that cannot be read (deleted or disabled)
// is not rotated. A failed rotation stays due and is retried by the next scan.
func (s *secretsService) RotateDueSecrets(ctx context.Context) (*schedulerhandlers.SecretsRotationResult, error) {
	sysCtx := auth.NewSystemContext(ctx)

	paths, err := s.secretsManager.List(sysCtx, "")
	if err != nil {
		return nil, &SecretsRotationScanError{Cause: err}
	}

	now := time.Now().UTC()
	result := &schedulerhandlers.SecretsRotationResult{}
	for _, path := range paths {
		data, metadata, err := s.secretsManager.Get(sysCtx, path)
		if err != nil {
			// A secret deleted since the list is not due. Any other
			// failure leaves its rotation unknown: count it and say which.
			if stderrors.Is(err, secrets.ErrSecretNotFound) || stderrors.Is(err, secrets.ErrSecretDeleted) {
				continue
			}
			result.Unreadable++
			if s.logger != nil {
				s.logger.WithFields(map[string]interface{}{
					"secret_id": path,
					"error":     err.Error(),
				}).Warn("Scheduled secret rotation could not read the secret")
			}
			continue
		}
		// A disabled secret is not rotated: GetSecret refuses it, and
		// the scan reads beneath that check.
		if metadata["status"] == string(SecretStatusDisabled) {
			continue
		}
		if !secretRotationDue(metadata, now) {
			continue
		}
		result.Due++
		if err := s.rotateDueSecret(secretsRotationWriteContext(sysCtx, path), path, data, metadata, now); err != nil {
			result.Failed++
			if s.logger != nil {
				s.logger.WithFields(map[string]interface{}{
					"secret_id": path,
					"error":     err.Error(),
				}).Warn("Scheduled secret rotation failed")
			}
			continue
		}
		result.Rotated++
	}
	return result, nil
}

// secretRotationDue reports whether metadata carries an enabled rotation
// policy whose next rotation is at or before now.
func secretRotationDue(metadata map[string]string, now time.Time) bool {
	if enabled, _ := strconv.ParseBool(metadata[secretRotationEnabledKey]); !enabled {
		return false
	}
	next, err := time.Parse(time.RFC3339, metadata[secretRotationNextKey])
	if err != nil || next.IsZero() {
		// An enabled policy with no deadline (one set before the service
		// scheduled the first) is due: the rotation schedules the next.
		return true
	}
	return !next.After(now)
}

// secretsRotationWriteContext is the context a scheduled rotation writes
// under: the tenant that owns the scoped path, as that tenant's own
// RotateSecret would, or the system tenant for an unscoped path.
func secretsRotationWriteContext(sysCtx context.Context, scopedPath string) context.Context {
	rest, ok := strings.CutPrefix(scopedPath, secretsTenantPrefix)
	if !ok {
		return tenantctx.WithSystem(sysCtx)
	}
	tenantID, _, ok := strings.Cut(rest, secretsTenantSeparator)
	if !ok || tenantID == "" {
		return tenantctx.WithSystem(sysCtx)
	}
	return tenantctx.WithTenant(sysCtx, tenantID)
}

// nextSecretRotation is the rotation after one performed at now: the policy's
// next rotation advanced by whole intervals until it is in the future, so a
// scan that missed several periods rotates once and keeps the cadence.
func nextSecretRotation(previous time.Time, interval time.Duration, now time.Time) time.Time {
	if previous.IsZero() {
		// No deadline was ever scheduled: the cadence starts now.
		return now.Add(interval)
	}
	next := previous.Add(interval)
	if !next.After(now) {
		missed := now.Sub(previous) / interval
		next = previous.Add((missed + 1) * interval)
	}
	return next
}

func (s *secretsService) rotateDueSecret(ctx context.Context, path string, data map[string][]byte, metadata map[string]string, now time.Time) error {
	interval, err := time.ParseDuration(metadata[secretRotationIntervalKey])
	if err != nil || interval <= 0 {
		return &SecretRotationIntervalError{Path: path, Interval: metadata[secretRotationIntervalKey]}
	}
	read, err := strconv.Atoi(metadata[secretVersionKey])
	if err != nil || read <= 0 {
		return &SecretVersionNotReportedError{Operation: "rotation read", SecretID: path}
	}
	storer, ok := s.secretsManager.(secretsConditionalStorer)
	if !ok {
		return &SecretVersionNotReportedError{Operation: "rotation write", SecretID: path}
	}
	previous, _ := time.Parse(time.RFC3339, metadata[secretRotationNextKey])

	newMetadata := make(map[string]string, len(metadata)+2)
	for k, v := range metadata {
		newMetadata[k] = v
	}
	delete(newMetadata, secretVersionKey)
	newMetadata[secretRotationReasonKey] = secretRotationScheduled
	newMetadata[secretRotationLastKey] = now.Format(time.RFC3339)
	newMetadata[secretRotationNextKey] = nextSecretRotation(previous, interval, now).Format(time.RFC3339)

	// The write holds only while the secret is still at the version this
	// scan read: an update, a policy change, a delete or a destroy and
	// recreation that lands between the read and the write wins, and the
	// rotation stores nothing.
	expected := secrets.ReadVersion{Version: read, Data: data, Metadata: metadata}
	newVersion, err := storer.StoreVersionIfCurrent(ctx, path, expected, data, newMetadata)
	if err != nil {
		return err
	}
	if s.metrics != nil {
		s.metrics.secretsRotatedTotal.Inc()
	}
	// Every interval adds a version, so the rotation bounds what it adds:
	// the write already holds, so a failed prune is logged and the next
	// rotation retries it.
	if pruner, ok := s.secretsManager.(secretsVersionPruner); ok {
		if _, perr := pruner.PruneVersions(ctx, path, rotationKeep(newMetadata)); perr != nil && !stderrors.Is(perr, secrets.ErrPruneUnsupported) && s.logger != nil {
			s.logger.WithFields(map[string]interface{}{
				"secret_id": path,
				"error":     perr.Error(),
			}).Warn("Rotated secret's old versions not pruned")
		}
	}
	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"secret_id":     path,
			"new_version":   newVersion,
			"rotation_next": newMetadata[secretRotationNextKey],
		}).Info("Secret rotated by its rotation policy")
	}
	return nil
}

// secretsConditionalStorer is the secrets manager's write conditional on
// the version it supersedes (secrets.Manager.StoreVersionIfCurrent).
type secretsConditionalStorer interface {
	StoreVersionIfCurrent(ctx context.Context, path string, expected secrets.ReadVersion, data map[string][]byte, metadata map[string]string) (int, error)
}
