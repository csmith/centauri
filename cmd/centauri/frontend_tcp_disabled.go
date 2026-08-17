//go:build notcp

package main

import "fmt"

// createTcpFrontend always errors: the binary has been built without the tcp frontend.
func createTcpFrontend() (frontend, error) {
	return nil, fmt.Errorf("tcp frontend is not compiled in (built with the notcp tag)")
}
