//go:build notcp

package frontend

import "fmt"

// NewTCP always errors: the binary has been built without the tcp frontend.
func NewTCP(httpPort, httpsPort int) (Frontend, error) {
	return nil, fmt.Errorf("tcp frontend is not compiled in (built with the notcp tag)")
}
