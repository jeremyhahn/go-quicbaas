//go:build raft && !integration

package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// Lifecycle stop methods — nil guard (early return) paths
// ---------------------------------------------------------------------------

func TestLifecycle_StopBackupManager_Nil(t *testing.T) {
	t.Run("nil backup manager returns nil", func(t *testing.T) {
		srv := withTempServer(t)
		srv.backupManager = nil
		err := srv.stopBackupManager()
		assert.NoError(t, err)
	})
}
