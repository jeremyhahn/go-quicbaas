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

package prompts

import (
	"context"
	"errors"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// SeedAuthor authors every copy a tenant is seeded with.
const SeedAuthor = "seed"

const (
	seedNote      = "copied from the system set"
	reconcileNote = "the embedded default changed"
	firstBootNote = "the embedded default"
)

// undoTimeout bounds the deletes that undo a failed seed. They run detached
// from the caller's context, which may be the one whose end failed the seed.
const undoTimeout = 30 * time.Second

// SeedReport lists the records one seeding or reconcile pass wrote.
type SeedReport struct {
	TenantID string
	Created  []Ref
	Updated  []Ref
}

// Changed reports whether the pass wrote anything.
func (r *SeedReport) Changed() bool {
	return len(r.Created) > 0 || len(r.Updated) > 0
}

// Seeder puts the system prompt set in place and copies it into tenants.
//
// The system set is the embedded defaults, written under the system tenant
// as records that follow the build: when a build ships a new default, the
// boot reconcile writes it as a new revision. A tenant's copy is made when
// the tenant is created, and on boot for any tenant that predates the store;
// every copy follows its parent, so resolution reads through it until
// somebody edits it. A record somebody edited never follows, and nothing
// here touches it.
type Seeder struct {
	store  *Store
	author string
}

// NewSeeder builds a seeder over store. build names the binary — its commit —
// and authors every system revision the reconcile writes.
func NewSeeder(store *Store, build string) (*Seeder, error) {
	if store == nil {
		return nil, &InvalidInputError{Field: "store", Message: "the seeder writes through the prompt store and cannot run without one"}
	}
	if build == "" {
		return nil, &InvalidInputError{Field: "build", Message: "system revisions name the build that wrote them"}
	}
	return &Seeder{store: store, author: "build " + build}, nil
}

// SeedTenant copies the system set into a newly created tenant. It is
// idempotent — a prompt the tenant already holds is left as it is — and on
// failure it deletes every record it wrote, so a rolled-back tenant creation
// leaves nothing behind. The system tenant is skipped: the boot reconcile
// owns it.
func (s *Seeder) SeedTenant(ctx context.Context, tenantID string) error {
	_, err := s.seedTenant(ctx, tenantID)
	return err
}

// Reconcile converges one tenant at boot: the system tenant onto this
// build's defaults, any other tenant by seeding the prompts it lacks.
func (s *Seeder) Reconcile(ctx context.Context, tenantID string) (*SeedReport, error) {
	if tenantID == qrdbsdk.SystemTenantID {
		return s.reconcileSystem(ctx)
	}
	return s.seedTenant(ctx, tenantID)
}

// seedTenant copies every prompt the tenant lacks from the system set.
func (s *Seeder) seedTenant(ctx context.Context, tenantID string) (*SeedReport, error) {
	report := &SeedReport{TenantID: tenantID}
	if tenantID == "" || tenantID == qrdbsdk.SystemTenantID {
		return report, nil
	}
	for _, ref := range s.refs(tenantID) {
		content, err := s.systemBody(ctx, ref)
		if err == nil {
			_, err = s.store.Save(ctx, ref, Change{
				Stamp:        Stamp{Author: SeedAuthor, Note: seedNote},
				Content:      content,
				FollowParent: true,
			})
		}
		if errors.Is(err, ErrVersionConflict) {
			// The tenant already holds this prompt.
			continue
		}
		if err != nil {
			return nil, s.undo(ctx, report.Created, &SeedError{TenantID: tenantID, Set: ref.Set, Prompt: ref.Name, Cause: err})
		}
		report.Created = append(report.Created, ref)
	}
	return report, nil
}

// systemBody is the body a tenant's copy starts from: the system record's,
// or the embedded default before the system set was ever written.
func (s *Seeder) systemBody(ctx context.Context, ref Ref) (string, error) {
	record, err := s.store.Get(ctx, Ref{TenantID: qrdbsdk.SystemTenantID, Set: ref.Set, Name: ref.Name})
	if errors.Is(err, ErrNotFound) {
		spec, specErr := s.store.spec(ref)
		return spec.Default, specErr
	}
	if err != nil {
		return "", err
	}
	return record.Content, nil
}

// undo deletes what a failed seed wrote. A record that will not delete is
// joined onto the cause rather than hidden, so the operator learns the
// tenant was left partly seeded.
func (s *Seeder) undo(ctx context.Context, created []Ref, failure *SeedError) error {
	// The seed may have failed because ctx ended; the deletes still run.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), undoTimeout)
	defer cancel()
	for _, ref := range created {
		if err := s.store.Delete(ctx, ref); err != nil {
			failure.Cause = errors.Join(failure.Cause, err)
		}
	}
	return failure
}

// reconcileSystem writes the embedded default into every system record that
// is missing or follows an older build. A record somebody edited is left
// alone. A failure stops the pass; what was written is already correct and
// the next boot finishes the rest.
func (s *Seeder) reconcileSystem(ctx context.Context) (*SeedReport, error) {
	report := &SeedReport{TenantID: qrdbsdk.SystemTenantID}
	for _, ref := range s.refs(qrdbsdk.SystemTenantID) {
		spec, err := s.store.spec(ref)
		if err != nil {
			return nil, &SeedError{TenantID: ref.TenantID, Set: ref.Set, Prompt: ref.Name, Cause: err}
		}
		record, err := s.store.Get(ctx, ref)
		change := Change{Stamp: Stamp{Author: s.author, Note: firstBootNote}, Content: spec.Default, FollowParent: true}
		switch {
		case errors.Is(err, ErrNotFound):
			report.Created = append(report.Created, ref)
		case err != nil:
			return nil, &SeedError{TenantID: ref.TenantID, Set: ref.Set, Prompt: ref.Name, Cause: err}
		case !record.FollowParent, record.ContentHash == Hash(spec.Default):
			continue
		default:
			change.ExpectedVersion = record.Version
			change.Note = reconcileNote
			report.Updated = append(report.Updated, ref)
		}
		if _, err := s.store.Save(ctx, ref, change); err != nil {
			return nil, &SeedError{TenantID: ref.TenantID, Set: ref.Set, Prompt: ref.Name, Cause: err}
		}
	}
	return report, nil
}

// refs lists the tenant-wide ref of every registered prompt.
func (s *Seeder) refs(tenantID string) []Ref {
	var refs []Ref
	for _, set := range s.store.registry.All() {
		for _, name := range set.Names() {
			refs = append(refs, Ref{TenantID: tenantID, Set: set.Kind(), Name: name})
		}
	}
	return refs
}
