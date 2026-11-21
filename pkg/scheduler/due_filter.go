package scheduler

import "context"

// DueJobFilter reports whether the node scanning for due jobs can dispatch
// job. A storage backend's due scan counts toward its limit, and returns,
// only the jobs the filter accepts, so jobs another node must run cannot
// fill the page and starve the jobs this node can run.
type DueJobFilter func(*Job) bool

// dueJobFilterKey is the context key under which a DueJobFilter travels.
type dueJobFilterKey struct{}

// WithDueJobFilter returns a context carrying f for
// JobStorage.GetJobsDueForExecution.
func WithDueJobFilter(ctx context.Context, f DueJobFilter) context.Context {
	return context.WithValue(ctx, dueJobFilterKey{}, f)
}

// DueJobFilterFrom returns the DueJobFilter ctx carries, or nil when it
// carries none (every due job is eligible).
func DueJobFilterFrom(ctx context.Context) DueJobFilter {
	f, _ := ctx.Value(dueJobFilterKey{}).(DueJobFilter)
	return f
}
