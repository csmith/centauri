//go:build integration

package main

import (
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/csmith/centauri/proxy"
	"github.com/stretchr/testify/assert"
)

func Test_NetworkConfigSource_ConnectsAndReceivesConfig(t *testing.T) {
	// Start a simple config server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NoError(t, err)
	defer listener.Close()

	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Send protocol header
		conn.Write([]byte("CENTAURI"))

		// Send version (4 bytes: 0x00 0x00 0x00 0x01)
		version := []byte{0x00, 0x00, 0x00, 0x01}
		conn.Write(version)

		// Send config payload
		payload := []byte("route example.com\n    upstream 127.0.0.1:8080\n")
		length := make([]byte, 4)
		binary.BigEndian.PutUint32(length, uint32(len(payload)))
		conn.Write(length)
		conn.Write(payload)

		// Keep connection open briefly
		time.Sleep(100 * time.Millisecond)
	}()

	source := newNetworkConfigSource()
	*configNetworkAddress = listener.Addr().String()

	routesCalled := make(chan struct{})
	updateRoutes := func(routes []*proxy.Route, fallback *proxy.Route) error {
		assert.Len(t, routes, 1)
		assert.Equal(t, "example.com", routes[0].Domains[0])
		close(routesCalled)
		return nil
	}

	errChan := make(chan error, 1)
	err = source.Start(updateRoutes, errChan)
	assert.NoError(t, err)
	defer source.Stop(nil)

	select {
	case <-routesCalled:
		// Success
	case err := <-errChan:
		t.Fatalf("Unexpected error: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for routes to be updated")
	}

	<-serverDone
}

func Test_NetworkConfigSource_ErrorsOnInvalidMagicBytes(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NoError(t, err)

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Send invalid magic bytes
		conn.Write([]byte("INVALID!"))

		// Close listener immediately so reconnection will fail
		listener.Close()
	}()

	source := newNetworkConfigSource()
	*configNetworkAddress = listener.Addr().String()

	errChan := make(chan error, 1)
	err = source.Start(func(routes []*proxy.Route, fallback *proxy.Route) error {
		return nil
	}, errChan)
	assert.NoError(t, err)
	defer source.Stop(nil)

	select {
	case err := <-errChan:
		assert.ErrorContains(t, err, "failed to reconnect")
	case <-time.After(2 * time.Second):
		t.Fatal("Expected error for invalid magic bytes")
	}
}

func Test_NetworkConfigSource_ErrorsOnUnsupportedVersion(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NoError(t, err)

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Send valid magic bytes
		conn.Write([]byte("CENTAURI"))

		// Send unsupported version (0x00 0x00 0x00 0x99)
		version := []byte{0x00, 0x00, 0x00, 0x99}
		conn.Write(version)

		// Close listener immediately so reconnection will fail
		listener.Close()
	}()

	source := newNetworkConfigSource()
	*configNetworkAddress = listener.Addr().String()

	errChan := make(chan error, 1)
	err = source.Start(func(routes []*proxy.Route, fallback *proxy.Route) error {
		return nil
	}, errChan)
	assert.NoError(t, err)
	defer source.Stop(nil)

	select {
	case err := <-errChan:
		assert.ErrorContains(t, err, "failed to reconnect")
	case <-time.After(2 * time.Second):
		t.Fatal("Expected error for unsupported version")
	}
}

func Test_NetworkConfigSource_RequiresAddress(t *testing.T) {
	source := newNetworkConfigSource()
	*configNetworkAddress = ""

	err := source.Start(func(routes []*proxy.Route, fallback *proxy.Route) error {
		return nil
	}, make(chan error))

	assert.ErrorContains(t, err, "address must be specified")
}

func Test_NetworkConfigSource_InitialConfigTimeout(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NoError(t, err)
	defer listener.Close()

	stopServer := make(chan struct{})
	defer close(stopServer)

	acceptCount := 0
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			acceptCount++
			go func(c net.Conn) {
				defer c.Close()
				// Don't send anything, just wait for test to finish or timeout
				select {
				case <-stopServer:
				case <-time.After(15 * time.Second):
				}
			}(conn)
			// After second connection, stop accepting
			if acceptCount >= 2 {
				return
			}
		}
	}()

	source := newNetworkConfigSource()
	*configNetworkAddress = listener.Addr().String()

	errChan := make(chan error, 1)
	err = source.Start(func(routes []*proxy.Route, fallback *proxy.Route) error {
		return nil
	}, errChan)
	assert.NoError(t, err)
	defer source.Stop(nil)

	select {
	case err := <-errChan:
		assert.ErrorContains(t, err, "failed to read config after reconnection")
	case <-time.After(25 * time.Second):
		// Should timeout within 10 seconds + reconnect + 10 seconds + small buffer
		t.Fatal("Expected timeout for initial config read")
	}
}
