//go:build notailscale

package main

import "fmt"

// createTailscaleFrontend always errors: the binary has been built without the tailscale frontend.
func createTailscaleFrontend() (frontend, error) {
	return nil, fmt.Errorf("tailscale frontend is not compiled in (built with the notailscale tag)")
}
