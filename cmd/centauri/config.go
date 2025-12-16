package main

import (
	"context"

	"github.com/csmith/centauri/proxy"
)

type routeUpdater func([]*proxy.Route, *proxy.Route) error

type configSource interface {
	Start(updateRoutes routeUpdater, errChan chan<- error) error
	Stop(ctx context.Context)
	Reload()
	Validate() error
}
