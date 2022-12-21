package main

import (
	"context"
	"sync"
	"time"

	"github.com/csmith/centauri/proxy"
)

const (
	shutdownTimeout = time.Second * 5
)

type frontend interface {
	Serve(manager *proxy.Manager, rewriter *proxy.Rewriter) error
	Stop(ctx context.Context)
}

var frontends = make(map[string]frontend)

func newBufferPool() *bufferPool {
	return &bufferPool{
		pool: sync.Pool{
			New: func() interface{} {
				return make([]byte, 32*1024)
			},
		},
	}
}

type bufferPool struct {
	pool sync.Pool
}

func (b *bufferPool) Get() []byte {
	return b.pool.Get().([]byte)
}

func (b *bufferPool) Put(bytes []byte) {
	b.pool.Put(bytes)
}
