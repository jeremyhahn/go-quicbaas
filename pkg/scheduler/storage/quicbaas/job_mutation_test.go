package quicbaas

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
)

// MutateJob writes a job change only over the record it decided against: a
// record that moved is re-read and re-decided, an absent job is never
// created, and a declined mutation writes nothing.
func TestClusteredStorage_MutateJob(t *testing.T) {
	started := time.Unix(1_800_000_000, 0)

	t.Run("a stale write is re-decided", func(t *testing.T) {
		s, kv := newSwappingStorage()
		ctx := context.Background()
		putRunningJob(t, kv, 1, started)
		kv.beforeSwap = func() {
			run := runOf(1, started)
			if won, err := s.ConcludeRun(ctx, run, concludeAs(run, "recovery@node-2")); err != nil || !won {
				t.Errorf("recovery between the read and the swap: won=%v err=%v", won, err)
			}
		}
		written, err := s.MutateJob(ctx, 1, runTenant, func(held *scheduler.Job) (*scheduler.Job, error) {
			next := *held
			next.Name = "renamed"
			return &next, nil
		})
		if err != nil || written == nil {
			t.Fatalf("MutateJob: job=%v err=%v", written, err)
		}
		job, ok := heldJob(t, kv, 1)
		if !ok || job.Status != scheduler.JobStatusPending || job.ConcludedBy != "recovery@node-2" || job.Name != "renamed" {
			t.Fatalf("record after a stale rename: %+v", job)
		}
	})

	t.Run("an absent job is not created", func(t *testing.T) {
		s, kv := newSwappingStorage()
		ran := false
		written, err := s.MutateJob(context.Background(), 9, runTenant, func(held *scheduler.Job) (*scheduler.Job, error) {
			ran = true
			next := *held
			return &next, nil
		})
		if written != nil || err != nil {
			t.Fatalf("MutateJob of an absent job: job=%v err=%v, want nil, nil", written, err)
		}
		if ran {
			t.Fatal("the mutation ran for an absent job")
		}
		if _, ok := heldJob(t, kv, 9); ok {
			t.Fatal("MutateJob created an absent job")
		}
	})

	t.Run("a declined mutation writes nothing", func(t *testing.T) {
		s, kv := newSwappingStorage()
		putRunningJob(t, kv, 1, started)
		declined := errors.New("declined")
		written, err := s.MutateJob(context.Background(), 1, runTenant, func(*scheduler.Job) (*scheduler.Job, error) {
			return nil, declined
		})
		if written != nil || !errors.Is(err, declined) {
			t.Fatalf("declined MutateJob: job=%v err=%v, want the mutation's error", written, err)
		}
		job, ok := heldJob(t, kv, 1)
		if !ok || job.Status != scheduler.JobStatusRunning || kv.swaps != 0 {
			t.Fatalf("record after a declined mutation: %+v swaps=%d", job, kv.swaps)
		}
	})
}
