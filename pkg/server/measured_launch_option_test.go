package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/attestationtest"
)

// TestWithMeasuredLaunch_StoredOnServerOptions proves the host's measured
// launch configuration reaches the server, which hands it to the service
// builder.
func TestWithMeasuredLaunch_StoredOnServerOptions(t *testing.T) {
	srv, err := NewServer(config.Default(), WithMeasuredLaunch(service.MeasuredLaunchConfig{Builders: []string{"https://factory.example.com"}}))
	require.NoError(t, err)
	require.NotNil(t, srv.opts.measuredLaunch, "WithMeasuredLaunch must record the configuration on serverOptions")
	assert.Equal(t, []string{"https://factory.example.com"}, srv.opts.measuredLaunch.Builders)

	srv, err = NewServer(config.Default())
	require.NoError(t, err)
	assert.Nil(t, srv.opts.measuredLaunch, "measuredLaunch must be nil when WithMeasuredLaunch is not supplied")
}

// idleTPM and idleRuntime satisfy the measured runtime's constructor; the
// test never launches, so no method is reached.
type idleTPM struct{ orchestration.HostTPM }

type idleRuntime struct{ orchestration.ContainerRuntime }

// quietLogger discards every line.
type quietLogger struct{ interfaces.Logger }

func (quietLogger) Debug(string, ...any)                              {}
func (quietLogger) Debugf(string, ...interface{})                     {}
func (quietLogger) Info(string, ...any)                               {}
func (quietLogger) Infof(string, ...interface{})                      {}
func (quietLogger) Warn(string, ...any)                               {}
func (quietLogger) Warnf(string, ...interface{})                      {}
func (quietLogger) Error(string, ...any)                              {}
func (quietLogger) Errorf(string, ...interface{})                     {}
func (l quietLogger) WithField(string, interface{}) interfaces.Logger { return l }
func (l quietLogger) WithFields(map[string]interface{}) interfaces.Logger {
	return l
}
func (l quietLogger) WithError(error) interfaces.Logger { return l }

// TestSandboxContainerRuntime proves the sandbox takes the service's
// measured runtime when there is one, and the exec engine otherwise.
func TestSandboxContainerRuntime(t *testing.T) {
	base := &orchestration.ExecContainerRuntime{}

	got := sandboxContainerRuntime(base, nil)
	assert.Same(t, base, got, "without measured launch the sandbox keeps the exec engine")

	measured, err := service.NewMeasuredLaunchRuntime(attestationtest.NewMemoryStore(),
		service.MeasuredLaunchConfig{TPM: idleTPM{}, Runtime: idleRuntime{}}, quietLogger{})
	require.NoError(t, err)
	got = sandboxContainerRuntime(base, measured)
	assert.Same(t, measured, got, "with measured launch the sandbox launches through the service's runtime")
}
