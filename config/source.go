package config

import (
	"context"

	"github.com/csmith/centauri/proxy"
)

// RouteUpdater is used by sources to install a new set of routes. It has the same signature as
// proxy.Manager.SetRoutes.
type RouteUpdater func(ctx context.Context, routes []*proxy.Route, fallback *proxy.Route) error

// Source provides routes to the proxy manager from some origin, and may keep them updated over time.
type Source interface {
	// Start begins providing routes, invoking updateRoutes whenever a new set of routes becomes available.
	// Errors that occur after starting are reported on errChan. Start returns an error if the source
	// cannot be started at all.
	Start(ctx context.Context, updateRoutes RouteUpdater, errChan chan<- error) error
	// Stop shuts the source down.
	Stop(ctx context.Context)
	// Reload requests an immediate refresh of the routes, if the source supports it.
	Reload()
	// Validate checks that the source is correctly configured, without starting it.
	Validate() error
}
