package config

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net"
	"time"
)

const (
	// magicBytes are sent at the start of every connection, to identify the protocol.
	magicBytes = "CENTAURI"
	// protocolVersion is the version of the network config protocol implemented by this client.
	protocolVersion = 0x01

	defaultReconnectInterval    = 100 * time.Millisecond
	defaultInitialConfigTimeout = 10 * time.Second
)

// NetworkSource reads routes from a server speaking the network config protocol: an 8-byte magic
// header, a 4-byte protocol version, a 4-byte big-endian payload length, and then the payload in the
// standard routes config format. See docs/network-config.md for details.
type NetworkSource struct {
	address              string
	reconnectInterval    time.Duration
	initialConfigTimeout time.Duration
	cancel               context.CancelFunc
	done                 chan struct{}
}

// NewNetworkSource creates a source that reads routes from the config server at the given address.
func NewNetworkSource(address string) *NetworkSource {
	return &NetworkSource{
		address:              address,
		reconnectInterval:    defaultReconnectInterval,
		initialConfigTimeout: defaultInitialConfigTimeout,
	}
}

func (n *NetworkSource) Start(ctx context.Context, updateRoutes RouteUpdater, errChan chan<- error) error {
	if n.address == "" {
		return fmt.Errorf("address must be specified when using network config source")
	}

	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", n.address)
	if err != nil {
		return fmt.Errorf("failed to connect to config server: %w", err)
	}

	stopCtx, cancel := context.WithCancel(ctx)
	n.cancel = cancel
	n.done = make(chan struct{})

	go func() {
		defer close(n.done)
		if err := n.run(ctx, stopCtx, conn, updateRoutes); err != nil {
			select {
			case errChan <- err:
			case <-stopCtx.Done():
			}
		}
	}()
	return nil
}

// Stop closes the connection to the config server and waits for the source to finish, or for ctx
// to be done.
func (n *NetworkSource) Stop(ctx context.Context) {
	if n.cancel == nil {
		return
	}

	n.cancel()
	select {
	case <-n.done:
	case <-ctx.Done():
	}
}

func (n *NetworkSource) Reload() {
	slog.Info("Reloading is not supported for network config source")
}

func (n *NetworkSource) Validate() error {
	return fmt.Errorf("validation is not supported for network config source")
}

// run reads configs until an unrecoverable error occurs or stopCtx is done. Routes are applied using
// ctx, so that stopping the source doesn't cancel anything started by updateRoutes.
func (n *NetworkSource) run(ctx, stopCtx context.Context, conn net.Conn, updateRoutes RouteUpdater) error {
	stopWatching := closeOnDone(stopCtx, conn)
	defer func() {
		stopWatching()
		_ = conn.Close()
	}()

	initialConfigRead := false
	secondChance := false
	for {
		if !initialConfigRead {
			if err := conn.SetDeadline(time.Now().Add(n.initialConfigTimeout)); err != nil {
				return fmt.Errorf("failed to set initial config read timeout: %w", err)
			}
		}

		err := n.readAndApplyConfig(ctx, conn, updateRoutes)
		if stopCtx.Err() != nil {
			return nil
		}

		if err == nil {
			if !initialConfigRead {
				initialConfigRead = true
				if err := conn.SetDeadline(time.Time{}); err != nil {
					return fmt.Errorf("failed to clear initial config read timeout: %w", err)
				}
			}
			secondChance = false
			continue
		}

		slog.Warn("Error reading config from network", "error", err)
		if secondChance {
			return fmt.Errorf("failed to read config after reconnection: %w", err)
		}

		stopWatching()
		_ = conn.Close()

		newConn, err := n.reconnect(stopCtx)
		if err != nil {
			if stopCtx.Err() != nil {
				return nil
			}
			return fmt.Errorf("failed to reconnect to config server: %w", err)
		}

		conn = newConn
		stopWatching = closeOnDone(stopCtx, conn)
		secondChance = true
	}
}

func (n *NetworkSource) reconnect(ctx context.Context) (net.Conn, error) {
	select {
	case <-time.After(n.reconnectInterval):
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", n.address)
	if err != nil {
		return nil, err
	}

	slog.Info("Reconnected to config server", "address", n.address)
	return conn, nil
}

// closeOnDone closes conn when ctx is done (immediately, if it already is), interrupting any
// blocked reads.
func closeOnDone(ctx context.Context, conn net.Conn) (stop func() bool) {
	return context.AfterFunc(
		ctx,
		func() {
			_ = conn.Close()
		},
	)
}

func (n *NetworkSource) readAndApplyConfig(ctx context.Context, conn net.Conn, updateRoutes RouteUpdater) error {
	// Magic header (8 bytes)
	magic := make([]byte, 8)
	if _, err := io.ReadFull(conn, magic); err != nil {
		return fmt.Errorf("failed to read magic bytes: %w", err)
	}

	if string(magic) != magicBytes {
		return fmt.Errorf("invalid magic bytes: got %q, expected %q", string(magic), magicBytes)
	}

	// Version header (4 bytes)
	versionHeader := make([]byte, 4)
	if _, err := io.ReadFull(conn, versionHeader); err != nil {
		return fmt.Errorf("failed to read version header: %w", err)
	}

	if versionHeader[0] != 0x00 || versionHeader[1] != 0x00 || versionHeader[2] != 0x00 || versionHeader[3] != protocolVersion {
		return fmt.Errorf("unsupported protocol version: %v", versionHeader)
	}

	// Payload length (4 bytes, big-endian)
	lengthBytes := make([]byte, 4)
	if _, err := io.ReadFull(conn, lengthBytes); err != nil {
		return fmt.Errorf("failed to read payload length: %w", err)
	}

	payloadLength := binary.BigEndian.Uint32(lengthBytes)

	payload := make([]byte, payloadLength)
	if _, err := io.ReadFull(conn, payload); err != nil {
		return fmt.Errorf("failed to read payload: %w", err)
	}

	slog.Debug("Received config from network", "size", payloadLength)

	routes, fallback, err := Parse(bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	slog.Debug("Installing routes from network config", "count", len(routes))
	if err := updateRoutes(ctx, routes, fallback); err != nil {
		return fmt.Errorf("route manager error: %w", err)
	}

	slog.Debug("Finished installing routes from network config", "count", len(routes))
	return nil
}
