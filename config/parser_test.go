package config

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_Parse_ReturnsEmptySliceForEmptyFile(t *testing.T) {
	routes, err := Parse(bytes.NewBuffer([]byte("")))

	assert.NoError(t, err)
	assert.Equal(t, 0, len(routes))
}

func Test_Parse_ErrorsOnUnknownLine(t *testing.T) {
	_, err := Parse(bytes.NewBuffer([]byte("error please")))

	assert.Error(t, err)
}

func Test_Parse_ErrorsOnUpstreamOutsideOfRoute(t *testing.T) {
	_, err := Parse(bytes.NewBuffer([]byte("upstream localhost:8080")))

	assert.Error(t, err)
}

func Test_Parse_ReturnsRoutes(t *testing.T) {
	routes, err := Parse(bytes.NewBuffer([]byte(`
route example.com www.example.com
    upstream localhost:8080

route example.net
	upstream localhost:8081
`)))

	assert.NoError(t, err)
	assert.Equal(t, 2, len(routes))
	assert.Equal(t, []string{"example.com", "www.example.com"}, routes[0].Domains)
	assert.Equal(t, "localhost:8080", routes[0].Upstream)
	assert.Equal(t, []string{"example.net"}, routes[1].Domains)
	assert.Equal(t, "localhost:8081", routes[1].Upstream)
}
