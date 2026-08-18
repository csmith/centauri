//go:build !notailscale

package frontend

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_Tailscale_errorsOnUnknownMode(t *testing.T) {
	ctx := newTestContext(t)

	frontend, err := NewTailscale(TailscaleOptions{Mode: "ftp"})
	require.NoError(t, err)

	err = frontend.Serve(ctx)
	assert.ErrorContains(t, err, "unknown value for tailscale mode")
}

func Test_Tailscale_UsesCertificatesDependsOnMode(t *testing.T) {
	httpFrontend, err := NewTailscale(TailscaleOptions{Mode: "http"})
	require.NoError(t, err)
	assert.False(t, httpFrontend.UsesCertificates())

	httpsFrontend, err := NewTailscale(TailscaleOptions{Mode: "https"})
	require.NoError(t, err)
	assert.True(t, httpsFrontend.UsesCertificates())
}
