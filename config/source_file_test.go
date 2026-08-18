package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/csmith/centauri/proxy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validFileConfig = "route example.com\n    upstream 127.0.0.1:8080\n"

func writeConfigFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "centauri.conf")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func waitForRoutes(t *testing.T, updates chan []*proxy.Route) []*proxy.Route {
	t.Helper()
	select {
	case routes := <-updates:
		return routes
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for routes to be updated")
		return nil
	}
}

func noopUpdater(_ context.Context, _ []*proxy.Route, _ *proxy.Route) error {
	return nil
}

func Test_FileSource_StartAppliesConfigFromFile(t *testing.T) {
	source := NewFileSource(writeConfigFile(t, validFileConfig))

	updates := make(chan []*proxy.Route, 1)
	err := source.Start(t.Context(), func(_ context.Context, routes []*proxy.Route, _ *proxy.Route) error {
		updates <- routes
		return nil
	}, make(chan error, 1))
	require.NoError(t, err)
	defer source.Stop(t.Context())

	routes := waitForRoutes(t, updates)
	require.Len(t, routes, 1)
	assert.Equal(t, "example.com", routes[0].Domains[0])
	assert.Equal(t, "127.0.0.1:8080", routes[0].Upstreams[0].Host)
}

func Test_FileSource_ReloadRereadsTheFile(t *testing.T) {
	path := writeConfigFile(t, validFileConfig)
	source := NewFileSource(path)

	updates := make(chan []*proxy.Route, 1)
	err := source.Start(t.Context(), func(_ context.Context, routes []*proxy.Route, _ *proxy.Route) error {
		updates <- routes
		return nil
	}, make(chan error, 1))
	require.NoError(t, err)
	defer source.Stop(t.Context())

	routes := waitForRoutes(t, updates)
	require.Len(t, routes, 1)
	assert.Equal(t, "example.com", routes[0].Domains[0])

	require.NoError(t, os.WriteFile(path, []byte("route example.org\n    upstream 127.0.0.1:8081\n"), 0o600))
	source.Reload()

	routes = waitForRoutes(t, updates)
	require.Len(t, routes, 1)
	assert.Equal(t, "example.org", routes[0].Domains[0])
}

func Test_FileSource_ReloadCoalescesPendingUpdates(t *testing.T) {
	source := NewFileSource(writeConfigFile(t, validFileConfig))

	source.Reload()
	source.Reload()
	assert.Len(t, source.updateChan, 1)
}

func Test_FileSource_reportsErrorsForMissingFile(t *testing.T) {
	source := NewFileSource(filepath.Join(t.TempDir(), "missing.conf"))

	errChan := make(chan error, 1)
	require.NoError(t, source.Start(t.Context(), noopUpdater, errChan))
	defer source.Stop(t.Context())

	select {
	case err := <-errChan:
		assert.ErrorContains(t, err, "failed to open config file")
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for error")
	}
}

func Test_FileSource_reportsErrorsForInvalidConfig(t *testing.T) {
	source := NewFileSource(writeConfigFile(t, "upstream 127.0.0.1:8080\n"))

	errChan := make(chan error, 1)
	require.NoError(t, source.Start(t.Context(), noopUpdater, errChan))
	defer source.Stop(t.Context())

	select {
	case err := <-errChan:
		assert.ErrorContains(t, err, "failed to parse config file")
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for error")
	}
}

func Test_FileSource_reportsErrorsFromRouteUpdater(t *testing.T) {
	source := NewFileSource(writeConfigFile(t, validFileConfig))

	errChan := make(chan error, 1)
	require.NoError(t, source.Start(t.Context(), func(_ context.Context, _ []*proxy.Route, _ *proxy.Route) error {
		return assert.AnError
	}, errChan))
	defer source.Stop(t.Context())

	select {
	case err := <-errChan:
		assert.ErrorContains(t, err, "route manager error")
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for error")
	}
}

func Test_FileSource_Validate(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr string
	}{
		{"valid config", validFileConfig, ""},
		{"missing file", "\x00missing", "failed to open config file"},
		{"invalid config", "upstream 127.0.0.1:8080\n", "failed to parse config file"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var path string
			if tt.content == "\x00missing" {
				path = filepath.Join(t.TempDir(), "missing.conf")
			} else {
				path = writeConfigFile(t, tt.content)
			}

			err := NewFileSource(path).Validate()
			if tt.wantErr == "" {
				assert.NoError(t, err)
			} else {
				assert.ErrorContains(t, err, tt.wantErr)
			}
		})
	}
}
