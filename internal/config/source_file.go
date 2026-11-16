package config

import (
	"context"
	"fmt"
	"log/slog"
	"os"
)

// FileSource reads routes from a file on disk, re-reading it every time Reload is called.
type FileSource struct {
	path       string
	updateChan chan struct{}
	stopChan   chan struct{}
}

// NewFileSource creates a source that reads routes from the file at the given path.
func NewFileSource(path string) *FileSource {
	return &FileSource{
		path:       path,
		updateChan: make(chan struct{}, 1),
		stopChan:   make(chan struct{}, 1),
	}
}

func (f *FileSource) Start(ctx context.Context, updateRoutes RouteUpdater, errChan chan<- error) error {
	go f.run(ctx, updateRoutes, errChan)
	f.Reload()
	return nil
}

func (f *FileSource) Stop(ctx context.Context) {
	f.stopChan <- struct{}{}
}

func (f *FileSource) Reload() {
	select {
	case f.updateChan <- struct{}{}:
		slog.Info("Scheduled config update")
	default:
		slog.Info("A config update was already scheduled; ignoring...")
	}
}

func (f *FileSource) Validate() error {
	slog.Debug("Validating config file", "path", f.path)

	configFile, err := os.Open(f.path)
	if err != nil {
		return fmt.Errorf("failed to open config file: %w", err)
	}
	defer configFile.Close()

	_, _, err = Parse(configFile)
	if err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}

	slog.Info("Config file is valid", "path", f.path)
	return nil
}

func (f *FileSource) run(ctx context.Context, updateRoutes RouteUpdater, errChan chan<- error) {
	for {
		select {
		case <-f.stopChan:
			return
		case <-f.updateChan:
			(func() {
				slog.Debug("Reading config file", "path", f.path)

				configFile, err := os.Open(f.path)
				if err != nil {
					errChan <- fmt.Errorf("failed to open config file: %w", err)
					return
				}
				defer configFile.Close()

				routes, fallback, err := Parse(configFile)
				if err != nil {
					errChan <- fmt.Errorf("failed to parse config file: %w", err)
					return
				}

				slog.Debug("Installing routes", "count", len(routes))
				if err := updateRoutes(ctx, routes, fallback); err != nil {
					errChan <- fmt.Errorf("route manager error: %w", err)
					return
				}
				slog.Debug("Finished installing routes", "count", len(routes))
			})()
		}
	}
}
