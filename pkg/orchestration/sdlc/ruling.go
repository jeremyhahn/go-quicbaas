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

package sdlc

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Owner rulings — a comment on a parked issue readmits its task
// =============================================================================
//
// A task parked needs_info or escalated waits on a person, and the person
// answers where the question was asked: on the issue. Before this existed the
// answer reached the issue and the task stayed parked, because nothing read
// it — the owner had to find the task in the API as well, and on go-codec#24
// found no way to (go-quicbaas#1020).
//
// A RULING IS A COMMENT BY AN AUTHORIZED RULER, POSTED AFTER THE PARK, THAT
// THE FACTORY DID NOT WRITE ITSELF. Each of the three clauses excludes
// something real:
//
//   - authorized ruler: project agents, workers and the task's own bot comment
//     on issues all the time, and none of them may clear an owner decision.
//   - after the park: a comment that was already there when the task parked
//     is what the task parked ON, not an answer to it; and a task readmitted
//     on a ruling that parks again later is not readmitted a second time by
//     the same comment, because the new park is newer than it.
//   - not the factory's: the factory's own comments carry a
//     `<!-- quicbaas-sdlc:... -->` marker, and whichever account the
//     factory's forge token belongs to, its status comments are not rulings.
//
// Each readmission is audited on the task as an artifact naming the ruler
// and the comment, and that artifact is also what makes it idempotent: a
// comment that has already readmitted a task never readmits it again.

// DefaultRulers are the forge logins, beyond the repository's owner, whose
// comments are owner rulings when no other set is configured: the company's
// administrative agent, which relays the owner's decisions (handbook
// conventions/sdlc.md).
var DefaultRulers = []string{"company-agent"}

// factoryCommentMarker opens every comment the factory writes to an issue.
const factoryCommentMarker = "<!-- quicbaas-sdlc:"

// rulingArtifactPrefix names the audit artifact a ruling readmission records.
const rulingArtifactPrefix = "owner_ruling:"

// maxForgeLoginLength bounds a forge login. Gitea caps a username at 40
// characters and GitHub at 39; a longer value is not a login on any forge
// the module speaks to.
const maxForgeLoginLength = 40

// forgeLoginPattern is the shape of a forge login: letters, digits, '-', '_'
// and '.', starting and ending with a letter or digit. It is the
// intersection of what Gitea and GitHub accept, so a value outside it names
// nobody and is refused rather than silently matching no comment.
var forgeLoginPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?$`)

// ValidateForgeLogin refuses a value that cannot be a forge login. field
// names the offending setting in the error.
func ValidateForgeLogin(field, login string) error {
	switch {
	case login == "":
		return &InvalidInputError{Field: field, Message: "must name a forge login"}
	case len(login) > maxForgeLoginLength:
		return &InvalidInputError{Field: field, Message: "a forge login is at most " + strconv.Itoa(maxForgeLoginLength) + " characters"}
	case !forgeLoginPattern.MatchString(login):
		return &InvalidInputError{Field: field, Message: strconv.Quote(login) + " is not a forge login"}
	}
	return nil
}

// validateRulerList refuses a ruler list holding a malformed login or naming
// one login twice. Logins compare case-insensitively, as forges do, so
// "Owner" and "owner" are the same ruler written twice.
func validateRulerList(field string, rulers []string) error {
	seen := make(map[string]struct{}, len(rulers))
	for i, login := range rulers {
		entry := field + "[" + strconv.Itoa(i) + "]"
		if err := ValidateForgeLogin(entry, login); err != nil {
			return err
		}
		key := strings.ToLower(login)
		if _, dup := seen[key]; dup {
			return &InvalidInputError{Field: entry, Message: strconv.Quote(login) + " is listed twice"}
		}
		seen[key] = struct{}{}
	}
	return nil
}

// RulerPolicy is the node's configured ruler set: the forge logins, beyond a
// repository's owner and a binding's own Rulers, whose comment on a parked
// issue is an owner ruling.
//
// THE THREE LAYERS ADD, EXCEPT WHERE A TENANT REPLACES THE NODE DEFAULT. A
// project's binding can only widen the set its tenant grants, because a
// binding is written by a tenant admin and the node's configuration by the
// operator; a tenant entry replaces Default so an operator can take the
// company agent away from one tenant without taking it from all of them.
type RulerPolicy struct {
	// Default applies to every tenant without an entry in Tenants. nil takes
	// DefaultRulers; an empty, non-nil slice leaves the repository's owner
	// and the binding's rulers as the only ones.
	Default []string

	// Tenants replaces Default for the tenants it names. An empty list is a
	// real answer: that tenant has no node-configured rulers.
	Tenants map[string][]string
}

// Validate refuses a policy naming a malformed login or an empty tenant.
func (p RulerPolicy) Validate() error {
	if err := validateRulerList("rulers.default", p.Default); err != nil {
		return err
	}
	for tenantID, rulers := range p.Tenants {
		if strings.TrimSpace(tenantID) == "" {
			return &InvalidInputError{Field: "rulers.tenants", Message: "a tenant entry must name its tenant"}
		}
		if err := validateRulerList("rulers.tenants."+tenantID, rulers); err != nil {
			return err
		}
	}
	return nil
}

// For returns the node-configured rulers for one tenant: its own entry when
// it has one, Default otherwise, DefaultRulers when Default is nil.
func (p RulerPolicy) For(tenantID string) []string {
	if rulers, ok := p.Tenants[tenantID]; ok {
		return append([]string(nil), rulers...)
	}
	if p.Default == nil {
		return append([]string(nil), DefaultRulers...)
	}
	return append([]string(nil), p.Default...)
}

// rulerSet is the logins whose comments rule on a project's parked tasks:
// the repository's owner, the binding's own rulers and the node-configured
// ones, compared case-insensitively because forge logins are.
func rulerSet(binding *ProjectBinding, configured []string) map[string]struct{} {
	set := make(map[string]struct{}, len(configured)+1)
	add := func(login string) {
		if login = strings.ToLower(strings.TrimSpace(login)); login != "" {
			set[login] = struct{}{}
		}
	}
	if binding != nil {
		add(binding.Forge.Owner)
		for _, login := range binding.Rulers {
			add(login)
		}
	}
	for _, login := range configured {
		add(login)
	}
	return set
}

// awaitsRuling reports whether a task is parked on a decision a ruling
// clears: a needs_info or escalated park, or any park whose typed condition
// is owner_ruling. An environment fault or an operator hold is not cleared by
// a comment, whoever writes it.
func awaitsRuling(task *orchestration.Task) bool {
	if task == nil || task.Status != orchestration.TaskStatusBlocked {
		return false
	}
	switch task.BlockKind {
	case orchestration.TaskBlockKindNeedsInfo, orchestration.TaskBlockKindEscalated:
		return true
	}
	condition, err := ParseUnblockCondition(task.BlockedReason)
	return err == nil && condition.Kind == UnblockOwnerRuling
}

// findRuling returns the newest comment that rules on the task's current
// park, or false when there is none. comments are oldest first, as
// ForgeBackend.ListIssueComments returns them.
func findRuling(task *orchestration.Task, comments []ForgeComment, rulers map[string]struct{}) (ForgeComment, bool) {
	for i := len(comments) - 1; i >= 0; i-- {
		comment := comments[i]
		if !comment.Created.IsZero() && comment.Created.UnixNano() <= task.StatusChangedAt {
			// Oldest first: everything before this one is older still.
			break
		}
		if comment.Created.IsZero() || !isRuling(comment, rulers) {
			continue
		}
		if hasRulingArtifact(task, comment.ID) {
			return ForgeComment{}, false
		}
		return comment, true
	}
	return ForgeComment{}, false
}

// isRuling reports whether one comment is an authorized ruler's own words.
func isRuling(comment ForgeComment, rulers map[string]struct{}) bool {
	if strings.Contains(comment.Body, factoryCommentMarker) {
		return false
	}
	_, authorized := rulers[strings.ToLower(strings.TrimSpace(comment.Author))]
	return authorized
}

// rulingArtifactName names the audit artifact one ruling comment records.
func rulingArtifactName(commentID int64) string {
	return rulingArtifactPrefix + strconv.FormatInt(commentID, 10)
}

// hasRulingArtifact reports whether this comment has already readmitted the
// task once.
func hasRulingArtifact(task *orchestration.Task, commentID int64) bool {
	name := rulingArtifactName(commentID)
	for _, artifact := range task.OutputArtifacts {
		if artifact.Name == name {
			return true
		}
	}
	return false
}

// rulingArtifact is the audit record of one ruling readmission: who ruled,
// in which comment, on which issue.
func rulingArtifact(task *orchestration.Task, comment ForgeComment) orchestration.Artifact {
	path := ""
	issue := ""
	if task.Source != nil {
		path, issue = task.Source.URL, task.Source.Ref
	}
	if path == "" {
		// A URL artifact must point somewhere; the issue reference is the
		// most specific place left when the source recorded no URL.
		path = issue
	}
	path += "#issuecomment-" + strconv.FormatInt(comment.ID, 10)
	return orchestration.Artifact{
		Kind: orchestration.ArtifactKindURL,
		Name: rulingArtifactName(comment.ID),
		Ref:  orchestration.ArtifactRef{Path: path},
		Metadata: map[string]string{
			"ruler":      comment.Author,
			"comment_id": strconv.FormatInt(comment.ID, 10),
			"issue":      issue,
			"ruled_at":   comment.Created.UTC().Format("2006-01-02T15:04:05Z"),
		},
	}
}
