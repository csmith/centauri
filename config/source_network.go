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
	stopChan             chan struct{}
	conn                 net.Conn
	initialConfigRead    bool
	reconnectInterval    time.Duration
	initialConfigTimeout time.Duration
}

// NewNetworkSource creates a source that reads routes from the config server at the given address.
func NewNetworkSource(address string) *NetworkSource {
	return &NetworkSource{
		address:              address,
		stopChan:             make(chan struct{}, 1),
		reconnectInterval:    defaultReconnectInterval,
		initialConfigTimeout: defaultInitialConfigTimeout,
	}
}

func (n *NetworkSource) Start(ctx context.Context, updateRoutes RouteUpdater, errChan chan<- error) error {
	if n.address == "" {
		return fmt.Errorf("address must be specified when using network config source")
	}

	var err error
	n.conn, err = net.Dial("tcp", n.address)
	if err != nil {
		return fmt.Errorf("failed to connect to config server: %w", err)
	}

	go n.run(ctx, updateRoutes, errChan)
	return nil
}

func (n *NetworkSource) Stop(ctx context.Context) {
	n.stopChan <- struct{}{}
	if n.conn != nil {
		n.conn.Close()
	}
}

func (n *NetworkSource) Reload() {
	slog.Info("Reloading is not supported for network config source")
}

func (n *NetworkSource) Validate() error {
	return fmt.Errorf("validation is not supported for network config source")
}

func (n *NetworkSource) run(ctx context.Context, updateRoutes RouteUpdater, errChan chan<- error) {
	secondChance := false
	for {
		select {
		case <-n.stopChan:
			return
		default:
			if !n.initialConfigRead {
				if err := n.conn.SetDeadline(time.Now().Add(n.initialConfigTimeout)); err != nil {
					errChan <- fmt.Errorf("failed to set initial config read timeout: %w", err)
					return
				}
			}

			if err := n.readAndApplyConfig(ctx, updateRoutes); err != nil {
				slog.Warn("Error reading config from network", "error", err)

				if secondChance {
					errChan <- fmt.Errorf("failed to read config after reconnection: %w", err)
					return
				}

				if err := n.reconnect(); err != nil {
					errChan <- fmt.Errorf("failed to reconnect to config server: %w", err)
					return
				}

				secondChance = true
			} else {
				if !n.initialConfigRead {
					n.initialConfigRead = true
					if err := n.conn.SetDeadline(time.Time{}); err != nil {
						errChan <- fmt.Errorf("failed to clear initial config read timeout: %w", err)
						return
					}
				}
				secondChance = false
			}
		}
	}
}

func (n *NetworkSource) reconnect() error {
	if n.conn != nil {
		n.conn.Close()
	}

	time.Sleep(n.reconnectInterval)

	var err error
	n.conn, err = net.Dial("tcp", n.address)
	if err != nil {
		return err
	}

	slog.Info("Reconnected to config server", "address", n.address)
	return nil
}

func (n *NetworkSource) readAndApplyConfig(ctx context.Context, updateRoutes RouteUpdater) error {
	// Magic header (8 bytes)
	magic := make([]byte, 8)
	if _, err := io.ReadFull(n.conn, magic); err != nil {
		return fmt.Errorf("failed to read magic bytes: %w", err)
	}

	if string(magic) != magicBytes {
		n.conn.Close()
		return fmt.Errorf("invalid magic bytes: got %q, expected %q", string(magic), magicBytes)
	}

	// Version header (4 bytes)
	versionHeader := make([]byte, 4)
	if _, err := io.ReadFull(n.conn, versionHeader); err != nil {
		return fmt.Errorf("failed to read version header: %w", err)
	}

	if versionHeader[0] != 0x00 || versionHeader[1] != 0x00 || versionHeader[2] != 0x00 || versionHeader[3] != protocolVersion {
		n.conn.Close()
		return fmt.Errorf("unsupported protocol version: %v", versionHeader)
	}

	// Payload length (4 bytes, big-endian)
	lengthBytes := make([]byte, 4)
	if _, err := io.ReadFull(n.conn, lengthBytes); err != nil {
		return fmt.Errorf("failed to read payload length: %w", err)
	}

	payloadLength := binary.BigEndian.Uint32(lengthBytes)

	payload := make([]byte, payloadLength)
	if _, err := io.ReadFull(n.conn, payload); err != nil {
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
