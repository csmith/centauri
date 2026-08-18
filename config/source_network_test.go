package config

import (
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/csmith/centauri/proxy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeConfigFrame writes a full network config protocol frame to the given connection.
func writeConfigFrame(conn net.Conn, payload []byte) error {
	if _, err := conn.Write([]byte(magicBytes)); err != nil {
		return err
	}

	version := []byte{0x00, 0x00, 0x00, protocolVersion}
	if _, err := conn.Write(version); err != nil {
		return err
	}

	length := make([]byte, 4)
	binary.BigEndian.PutUint32(length, uint32(len(payload)))
	if _, err := conn.Write(length); err != nil {
		return err
	}

	_, err := conn.Write(payload)
	return err
}

func Test_NetworkSource_ConnectsAndReceivesConfig(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		assert.NoError(t, writeConfigFrame(conn, []byte(validFileConfig)))

		// Keep connection open briefly
		time.Sleep(100 * time.Millisecond)
	}()

	source := NewNetworkSource(listener.Addr().String())

	routesCalled := make(chan struct{})
	updateRoutes := func(_ context.Context, routes []*proxy.Route, fallback *proxy.Route) error {
		assert.Len(t, routes, 1)
		assert.Equal(t, "example.com", routes[0].Domains[0])
		assert.Nil(t, fallback)
		close(routesCalled)
		return nil
	}

	errChan := make(chan error, 1)
	err = source.Start(t.Context(), updateRoutes, errChan)
	require.NoError(t, err)
	defer func() {
		// Close listener first so the background goroutine can't reconnect,
		// then stop the source and wait for the goroutine to exit.
		listener.Close()
		source.Stop(t.Context())
		select {
		case <-errChan:
		case <-time.After(500 * time.Millisecond):
		}
	}()

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

func Test_NetworkSource_ErrorsOnInvalidMagicBytes(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Send invalid magic bytes
		_, _ = conn.Write([]byte("INVALID!"))

		// Close listener immediately so reconnection will fail
		listener.Close()
	}()

	source := NewNetworkSource(listener.Addr().String())

	errChan := make(chan error, 1)
	err = source.Start(t.Context(), noopUpdater, errChan)
	require.NoError(t, err)
	defer source.Stop(t.Context())

	select {
	case err := <-errChan:
		assert.ErrorContains(t, err, "failed to reconnect")
	case <-time.After(2 * time.Second):
		t.Fatal("Expected error for invalid magic bytes")
	}
}

func Test_NetworkSource_ErrorsOnUnsupportedVersion(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Send valid magic bytes
		_, _ = conn.Write([]byte(magicBytes))

		// Send unsupported version (0x00 0x00 0x00 0x99)
		_, _ = conn.Write([]byte{0x00, 0x00, 0x00, 0x99})

		// Close listener immediately so reconnection will fail
		listener.Close()
	}()

	source := NewNetworkSource(listener.Addr().String())

	errChan := make(chan error, 1)
	err = source.Start(t.Context(), noopUpdater, errChan)
	require.NoError(t, err)
	defer source.Stop(t.Context())

	select {
	case err := <-errChan:
		assert.ErrorContains(t, err, "failed to reconnect")
	case <-time.After(2 * time.Second):
		t.Fatal("Expected error for unsupported version")
	}
}

func Test_NetworkSource_RequiresAddress(t *testing.T) {
	source := NewNetworkSource("")

	err := source.Start(t.Context(), noopUpdater, make(chan error))

	assert.ErrorContains(t, err, "address must be specified")
}

func Test_NetworkSource_InitialConfigTimeout(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
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

	source := NewNetworkSource(listener.Addr().String())
	source.initialConfigTimeout = 50 * time.Millisecond
	source.reconnectInterval = 10 * time.Millisecond

	errChan := make(chan error, 1)
	err = source.Start(t.Context(), noopUpdater, errChan)
	require.NoError(t, err)
	defer source.Stop(t.Context())

	select {
	case err := <-errChan:
		assert.ErrorContains(t, err, "failed to read config after reconnection")
	case <-time.After(2 * time.Second):
		t.Fatal("Expected timeout for initial config read")
	}
}
