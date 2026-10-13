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

// FleetLoadReporter reports how much work the control plane has placed on each
// fleet node right now. The remote backend resolver ranks eligible fleet nodes by
// it, so work spreads across the fleet instead of piling onto whichever fleet node
// the registry happens to list first.
//
// THE CONTROL PLANE COUNTS, NOT THE AGENT. A fleet node's heartbeat carries its own
// active-session count, but the registry keeps no running copy of it; the
// control plane's live session records already name the host each session was
// placed on (sessionRecord.HostAffinity), and that is the count placement acts
// on the moment a session exists — not a heartbeat later.
type FleetLoadReporter interface {
	// LiveJobsByHost returns, per fleet node host, the number of jobs with a live
	// session there. A session that belongs to no job counts as one of its own.
	// Sessions this control plane runs on a local backend count nowhere.
	LiveJobsByHost() map[string]int
}

// Compile-time interface check.
var _ FleetLoadReporter = (*agentServiceImpl)(nil)

// LiveJobsByHost implements FleetLoadReporter over the live session records.
//
// JOBS, NOT SESSIONS, because a fleet node's advertised capacity is a job count
// (FleetNode.MaxConcurrentJobs): a job's lead, worker and reviewer
// sessions on one host occupy one of its slots, not three.
func (s *agentServiceImpl) LiveJobsByHost() map[string]int {
	jobs := map[string]map[string]struct{}{}
	s.sessions.Range(func(key, value any) bool {
		sessionID, isID := key.(string)
		rec, isRecord := value.(*sessionRecord)
		if !isID || !isRecord || rec.BackendLocal || rec.HostAffinity == "" {
			return true
		}
		work := "job:" + rec.JobID
		if rec.JobID == "" {
			work = "session:" + sessionID
		}
		onHost, ok := jobs[rec.HostAffinity]
		if !ok {
			onHost = map[string]struct{}{}
			jobs[rec.HostAffinity] = onHost
		}
		onHost[work] = struct{}{}
		return true
	})
	load := make(map[string]int, len(jobs))
	for host, work := range jobs {
		load[host] = len(work)
	}
	return load
}

// jobSlotHosts returns the agent hosts on which jobID has a live session: the
// hosts where LiveJobsByHost already counts the job. An empty jobID is no job.
func (s *agentServiceImpl) jobSlotHosts(jobID string) JobSlotHosts {
	if jobID == "" {
		return nil
	}
	hosts := JobSlotHosts{}
	s.sessions.Range(func(_, value any) bool {
		rec, isRecord := value.(*sessionRecord)
		if isRecord && !rec.BackendLocal && rec.HostAffinity != "" && rec.JobID == jobID {
			hosts[rec.HostAffinity] = struct{}{}
		}
		return true
	})
	return hosts
}
