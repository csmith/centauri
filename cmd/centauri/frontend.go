package main

import (
	"context"

	"github.com/csmith/centauri/proxy"
)

type frontend interface {
	Serve(manager *proxy.Manager, rewriter *proxy.Rewriter) error
	Stop(ctx context.Context)
}

var frontends = make(map[string]frontend)
