package quicbaas

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
)

// scanHookKV runs afterScan once, just after the first scan of execution
// history: whatever it does happened between a flush's history check and its
// write.
type scanHookKV struct {
	*swappingKV
	afterScan func()
}

func (w *scanHookKV) Scan(ctx context.Context, prefix string, fn func(key string, value []byte) error) error {
	err := w.swappingKV.Scan(ctx, prefix, fn)
	if strings.Contains(prefix, "scheduler/executions/") && w.afterScan != nil {
		h := w.afterScan
		w.afterScan = nil
		h()
	}
	return err
}

func refusedConcluded(err error) bool {
	var refused scheduler.RecoveryRefusedError
	return errors.As(err, &refused) && refused.Refusal == scheduler.RefusedConcluded
}

// TestRecoverAbandonedRun_TwoNodesFlushingOnePendingRecordWriteOneHistoryEntry:
// the winning recoverer and a second node's recovery both flush the run's
// pending record; production storage keeps one history entry.
func TestRecoverAbandonedRun_TwoNodesFlushingOnePendingRecordWriteOneHistoryEntry(t *testing.T) {
	ctx := context.Background()
	_, inner := newSwappingStorage()
	kv := &scanHookKV{swappingKV: inner}
	storage := &ClusteredStorage{Storage: &Storage{kv: kv}}

	firing := time.Now().Add(-time.Hour).Truncate(time.Second)
	started := firing
	job := scheduler.Job{
		ID: 7, Name: "pending-flush", OwnerType: scheduler.OwnerTypeTenant, OwnerID: runTenant,
		Type: scheduler.JobTypeOneShot, Handler: "noop", Status: scheduler.JobStatusRunning,
		NextRun: firing, RunStartedAt: started,
	}
	data, err := json.Marshal(&job)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := kv.Put(ctx, keyForJobByID(7, runTenant), data); err != nil {
		t.Fatalf("Put: %v", err)
	}

	a, err := scheduler.NewWithClustering(idTestConfig(), storage, "node-1", nil)
	if err != nil {
		t.Fatalf("NewWithClustering: %v", err)
	}
	b, err := scheduler.NewWithClustering(idTestConfig(), storage, "node-2", nil)
	if err != nil {
		t.Fatalf("NewWithClustering: %v", err)
	}
	att := scheduler.AbandonedRunAttestation{Firing: firing, Detector: "test", Detail: "no heartbeat"}

	held := func() *scheduler.Job {
		j, ok := heldJob(t, inner, 7)
		if !ok {
			t.Fatal("job 7 is not stored")
		}
		return j
	}

	// Node-1 has won the conclusion and checked the history; node-2's
	// recovery now reads the concluded job, flushes its pending record and
	// is refused.
	kv.afterScan = func() {
		if _, err := b.RecoverAbandonedRun(ctx, held(), att); !refusedConcluded(err) {
			t.Errorf("second node's recovery: err = %v, want refused as concluded", err)
		}
	}
	if _, err := a.RecoverAbandonedRun(ctx, held(), att); err != nil {
		t.Fatalf("winning recovery: %v", err)
	}
	if kv.afterScan != nil {
		t.Fatal("the second node's recovery never ran")
	}

	execs, err := storage.GetExecutions(ctx, 7, runTenant, 10)
	if err != nil {
		t.Fatalf("GetExecutions: %v", err)
	}
	if len(execs) != 1 {
		t.Errorf("history holds %d records for one run, want 1", len(execs))
	}
	if pending := held().PendingHistory; len(pending) != 0 {
		t.Errorf("job still holds %d pending records", len(pending))
	}
}
