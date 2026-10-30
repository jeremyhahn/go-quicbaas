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

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// SeedAuthor authors every copy a tenant is seeded with.
const SeedAuthor = "seed"

const (
	seedNote      = "copied from the system set"
	reconcileNote = "the embedded default changed"
	firstBootNote = "the embedded default"
)

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
	refs := s.refs(tenantID)
	bodies, err := s.systemBodies(ctx, refs)
	if err != nil {
		return nil, &SeedError{TenantID: tenantID, Cause: err}
	}
	for _, ref := range refs {
		_, err := s.store.Save(ctx, ref, Change{
			Stamp:        Stamp{Author: SeedAuthor, Note: seedNote},
			Content:      bodies[ref],
			FollowParent: true,
		})
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

// systemBodies maps each ref to the body a tenant's copy starts from: the
// system record's, or the embedded default before the system set was ever
// written. One scan reads the whole system set, so the cost of a seed does
// not grow by a round trip per prompt.
func (s *Seeder) systemBodies(ctx context.Context, refs []Ref) (map[Ref]string, error) {
	held, err := s.store.List(ctx, qrdbsdk.SystemTenantID, "", "")
	if err != nil {
		return nil, err
	}
	system := make(map[Ref]string, len(held))
	for _, record := range held {
		system[Ref{Set: record.Set, Name: record.Name}] = record.Content
	}
	bodies := make(map[Ref]string, len(refs))
	for _, ref := range refs {
		body, ok := system[Ref{Set: ref.Set, Name: ref.Name}]
		if !ok {
			spec, err := s.store.spec(ref)
			if err != nil {
				return nil, err
			}
			body = spec.Default
		}
		bodies[ref] = body
	}
	return bodies, nil
}

// undo deletes what a failed seed wrote. A record that will not delete is
// joined onto the cause rather than hidden, so the operator learns the
// tenant was left partly seeded.
func (s *Seeder) undo(ctx context.Context, created []Ref, failure *SeedError) error {
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
