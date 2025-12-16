package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"time"

	"github.com/csmith/centauri/config"
)

var (
	configNetworkAddress = flag.String("config-network-address", "", "Address to connect to for network config source")
)

const (
	magicBytes           = "CENTAURI"
	protocolVersion      = 0x01
	reconnectInterval    = 100 * time.Millisecond
	initialConfigTimeout = 10 * time.Second
)

type networkConfigSource struct {
	stopChan          chan struct{}
	conn              net.Conn
	initialConfigRead bool
}

func newNetworkConfigSource() *networkConfigSource {
	return &networkConfigSource{
		stopChan: make(chan struct{}, 1),
	}
}

func (n *networkConfigSource) Start(updateRoutes routeUpdater, errChan chan<- error) error {
	if *configNetworkAddress == "" {
		return fmt.Errorf("address must be specified when using network config source")
	}

	var err error
	n.conn, err = net.Dial("tcp", *configNetworkAddress)
	if err != nil {
		return fmt.Errorf("failed to connect to config server: %w", err)
	}

	go n.run(updateRoutes, errChan)
	return nil
}

func (n *networkConfigSource) Stop(ctx context.Context) {
	n.stopChan <- struct{}{}
	if n.conn != nil {
		n.conn.Close()
	}
}

func (n *networkConfigSource) Reload() {
	slog.Info("Reloading is not supported for network config source")
}

func (n *networkConfigSource) Validate() error {
	return fmt.Errorf("validation is not supported for network config source")
}

func (n *networkConfigSource) run(updateRoutes routeUpdater, errChan chan<- error) {
	secondChance := false
	for {
		select {
		case <-n.stopChan:
			return
		default:
			if !n.initialConfigRead {
				if err := n.conn.SetDeadline(time.Now().Add(initialConfigTimeout)); err != nil {
					errChan <- fmt.Errorf("failed to set initial config read timeout: %w", err)
					return
				}
			}

			if err := n.readAndApplyConfig(updateRoutes); err != nil {
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

func (n *networkConfigSource) reconnect() error {
	if n.conn != nil {
		n.conn.Close()
	}

	time.Sleep(reconnectInterval)

	var err error
	n.conn, err = net.Dial("tcp", *configNetworkAddress)
	if err != nil {
		return err
	}

	slog.Info("Reconnected to config server", "address", *configNetworkAddress)
	return nil
}

func (n *networkConfigSource) readAndApplyConfig(updateRoutes routeUpdater) error {
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
	if payloadLength == 0 {
		return fmt.Errorf("payload length is zero")
	}

	// Payload
	payload := make([]byte, payloadLength)
	if _, err := io.ReadFull(n.conn, payload); err != nil {
		return fmt.Errorf("failed to read payload: %w", err)
	}

	slog.Debug("Received config from network", "size", payloadLength)

	routes, fallback, err := config.Parse(bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	slog.Debug("Installing routes from network config", "count", len(routes))
	if err := updateRoutes(routes, fallback); err != nil {
		return fmt.Errorf("route manager error: %w", err)
	}

	slog.Debug("Finished installing routes from network config", "count", len(routes))
	return nil
}
