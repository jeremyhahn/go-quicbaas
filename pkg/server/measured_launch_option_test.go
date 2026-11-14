package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
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
