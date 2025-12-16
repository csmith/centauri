package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/csmith/centauri/config"
)

var (
	configPath = flag.String("config", "centauri.conf", "Path to config")
)

type fileConfigSource struct {
	updateChan chan struct{}
	stopChan   chan struct{}
}

func newFileConfigSource() *fileConfigSource {
	return &fileConfigSource{
		updateChan: make(chan struct{}, 1),
		stopChan:   make(chan struct{}, 1),
	}
}

func (f *fileConfigSource) Start(updateRoutes routeUpdater, errChan chan<- error) error {
	go f.run(updateRoutes, errChan)
	f.Reload()
	return nil
}

func (f *fileConfigSource) Stop(ctx context.Context) {
	f.stopChan <- struct{}{}
}

func (f *fileConfigSource) Reload() {
	select {
	case f.updateChan <- struct{}{}:
		slog.Info("Scheduled config update")
	default:
		slog.Info("A config update was already scheduled; ignoring...")
	}
}

func (f *fileConfigSource) Validate() error {
	slog.Debug("Validating config file", "path", *configPath)

	configFile, err := os.Open(*configPath)
	if err != nil {
		return fmt.Errorf("failed to open config file: %w", err)
	}
	defer configFile.Close()

	_, _, err = config.Parse(configFile)
	if err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}

	slog.Info("Config file is valid", "path", *configPath)
	return nil
}

func (f *fileConfigSource) run(updateRoutes routeUpdater, errChan chan<- error) {
	for {
		select {
		case <-f.stopChan:
			return
		case <-f.updateChan:
			(func() {
				slog.Debug("Reading config file", "path", *configPath)

				configFile, err := os.Open(*configPath)
				if err != nil {
					errChan <- fmt.Errorf("failed to open config file: %w", err)
					return
				}
				defer configFile.Close()

				routes, fallback, err := config.Parse(configFile)
				if err != nil {
					errChan <- fmt.Errorf("failed to parse config file: %w", err)
					return
				}

				slog.Debug("Installing routes", "count", len(routes))
				if err := updateRoutes(routes, fallback); err != nil {
					errChan <- fmt.Errorf("route manager error: %w", err)
					return
				}
				slog.Debug("Finished installing routes", "count", len(routes))
			})()
		}
	}
}
