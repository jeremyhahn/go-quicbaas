package scheduler

import "sort"

// Names of the platform-owned handlers. The handler packages import this
// package, so the names are spelled out here; a test in pkg/service pins
// them to the handler packages' own constants.
const (
	PlatformHandlerKMSRotation       = "kms.rotation"
	PlatformHandlerLedgerPricing     = "ledger.pricing"
	PlatformHandlerSDLCTokenRotation = "sdlc.token_rotation"
	PlatformHandlerAuditCleanup      = "audit.cleanup"
	PlatformHandlerDBCompaction      = "db.compaction"
)

// platformHandlers are the handlers that act for the platform as a whole:
// they sweep every tenant, or build a system context, and ignore the tenant
// of the job they run for. Only system-owned jobs may name them; a
// tenant-owned job of one is refused when it is created or updated, and
// again when it comes up for execution.
//
// A handler that ignores the job's tenant or builds a system context MUST be
// listed here.
var platformHandlers = map[string]struct{}{
	PlatformHandlerKMSRotation:       {},
	PlatformHandlerLedgerPricing:     {},
	PlatformHandlerSDLCTokenRotation: {},
	PlatformHandlerAuditCleanup:      {},
	PlatformHandlerDBCompaction:      {},
}

// IsPlatformHandler reports whether name is a platform-owned handler, one
// that only a system-owned job may run.
func IsPlatformHandler(name string) bool {
	_, ok := platformHandlers[name]
	return ok
}

// PlatformHandlers returns the platform-owned handler names, sorted.
func PlatformHandlers() []string {
	names := make([]string, 0, len(platformHandlers))
	for name := range platformHandlers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// checkPlatformHandler refuses a tenant-owned job that names a platform
// handler.
func checkPlatformHandler(job *Job) error {
	if tenantID := JobTenantID(job); tenantID != "" && IsPlatformHandler(job.Handler) {
		return PlatformHandlerError{Handler: job.Handler, TenantID: tenantID}
	}
	return nil
}
