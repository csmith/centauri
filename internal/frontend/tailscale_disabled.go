//go:build notailscale

package frontend

import "fmt"

// NewTailscale always errors: the binary has been built without the tailscale frontend.
func NewTailscale(options TailscaleOptions) (Frontend, error) {
	return nil, fmt.Errorf("tailscale frontend is not compiled in (built with the notailscale tag)")
}
