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

package orchestration

import (
	"context"
	"strconv"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/prompts"
)

// AuditPromptsResolved is the job audit event recording which prompt versions
// a job was launched with.
const AuditPromptsResolved = "prompts:resolved"

// PromptResolver chooses the prompt bodies a job renders from: project, then
// tenant, then system, then the embedded default. *prompts.Resolver is the
// production one.
type PromptResolver interface {
	Resolve(ctx context.Context, tenantID, projectID string, kind prompts.SetKind) (*prompts.ResolvedSet, error)
}

// embeddedPrompts resolves every prompt to the build's own default: the
// answer for a service nothing wired a store into.
var embeddedPrompts PromptResolver = prompts.NewResolver(nil)

// SetPromptResolver configures where jobs resolve their prompt sets. Unset,
// every job renders the embedded defaults.
func (s *agentServiceImpl) SetPromptResolver(resolver PromptResolver) {
	s.promptResolver = resolver
}

// resolveJobPrompts resolves set kind, once, for the job's tenant and
// project. A failure is returned as it came — a *prompts.ResolveError naming
// the prompt — and refuses the launch: a job must never run on instructions
// nobody can name.
func (s *agentServiceImpl) resolveJobPrompts(ctx context.Context, kind prompts.SetKind, config *JobConfig) (*prompts.ResolvedSet, error) {
	resolver := s.promptResolver
	if resolver == nil {
		resolver = embeddedPrompts
	}
	return resolver.Resolve(ctx, config.TenantID, config.ProjectID, kind)
}

// workflowRenderer is what the job renders the workflow prompts from: the set
// it was launched with, or the build's defaults for a job whose runner
// resolves another set — or none — and for a run built without a launch.
func (r *JobRun) workflowRenderer() prompts.Renderer {
	if r == nil {
		return workflowPrompts
	}
	return prompts.RendererOr(r.Prompts, workflowPrompts)
}

// promptsResolvedDetail is the audit line for a resolved set: the set, then
// each prompt something other than the build answered, by scope and version.
// It carries identifiers only — never a body.
func promptsResolvedDetail(set *prompts.ResolvedSet) string {
	var b strings.Builder
	b.WriteString("set=")
	b.WriteString(set.Kind().String())
	embedded := 0
	for _, ref := range set.Refs() {
		if ref.Scope == prompts.ScopeEmbedded {
			embedded++
			continue
		}
		b.WriteString(" ")
		b.WriteString(ref.Name)
		b.WriteString("=")
		b.WriteString(string(ref.Scope))
		if ref.ScopeID != "" {
			b.WriteString(":")
			b.WriteString(ref.ScopeID)
		}
		b.WriteString("@v")
		b.WriteString(strconv.FormatInt(ref.Version, 10))
	}
	b.WriteString(" embedded=")
	b.WriteString(strconv.Itoa(embedded))
	return b.String()
}
