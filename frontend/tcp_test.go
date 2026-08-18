//go:build !notcp

package frontend

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_TCP_ServeStartsBothServers(t *testing.T) {
	ctx := newTestContext(t)

	frontend, err := NewTCP(0, 0)
	require.NoError(t, err)
	assert.True(t, frontend.UsesCertificates())

	require.NoError(t, frontend.Serve(ctx))
	frontend.Stop(t.Context())
}
