package config

import (
	"bytes"
	"testing"

	"github.com/csmith/centauri/proxy"
	"github.com/stretchr/testify/assert"
)

func Test_Parse_ReturnsEmptySliceForEmptyFile(t *testing.T) {
	routes, _, err := Parse(bytes.NewBuffer([]byte("")))

	assert.NoError(t, err)
	assert.Equal(t, 0, len(routes))
}

func Test_Parse_ErrorsOnUnknownLine(t *testing.T) {
	_, _, err := Parse(bytes.NewBuffer([]byte("error please")))

	assert.ErrorContains(t, err, "invalid line")
}

func Test_Parse_ErrorsOnUpstreamOutsideOfRoute(t *testing.T) {
	_, _, err := Parse(bytes.NewBuffer([]byte("upstream localhost:8080")))

	assert.ErrorContains(t, err, "upstream without route")
}

func Test_Parse_ErrorsOnProviderOutsideOfRoute(t *testing.T) {
	_, _, err := Parse(bytes.NewBuffer([]byte("provider lego")))

	assert.ErrorContains(t, err, "provider without route")
}

func Test_Parse_ErrorsOnHeaderOutsideOfRoute(t *testing.T) {
	_, _, err := Parse(bytes.NewBuffer([]byte("header add x-test foo")))

	assert.ErrorContains(t, err, "header without route")
}

func Test_Parse_ErrorsOnHeaderWithNoParameters(t *testing.T) {
	_, _, err := Parse(bytes.NewBuffer([]byte(`
route example.com
	header nothing
`)))

	assert.ErrorContains(t, err, "invalid header operation")
}

func Test_Parse_ErrorsOnHeaderWithTooFewParameters(t *testing.T) {
	_, _, err := Parse(bytes.NewBuffer([]byte(`
route example.com
	header nothing
`)))

	assert.ErrorContains(t, err, "invalid header operation")
}

func Test_Parse_ErrorsOnRouteWithNoDomains(t *testing.T) {
	_, _, err := Parse(bytes.NewBuffer([]byte(`
route
	upstream example.com
`)))

	assert.ErrorContains(t, err, "no domains specified for route")
}

func Test_Parse_ErrorsOnIntermediateRouteWithoutUpstreams(t *testing.T) {
	_, _, err := Parse(bytes.NewBuffer([]byte(`
route example.com
route example.net
	upstream localhost:8080
`)))

	assert.ErrorContains(t, err, "no upstreams specified for route")
}

func Test_Parse_ErrorsOnFinalRouteWithoutUpstreams(t *testing.T) {
	_, _, err := Parse(bytes.NewBuffer([]byte(`
route example.com
`)))

	assert.ErrorContains(t, err, "no upstreams specified for route")
}

func Test_Parse_ErrorsOnHeaderAddWithoutValue(t *testing.T) {
	_, _, err := Parse(bytes.NewBuffer([]byte(`
route example.com
	header add x-test
`)))

	assert.ErrorContains(t, err, "invalid header add line")
}

func Test_Parse_ErrorsOnHeaderDeleteWithoutHeader(t *testing.T) {
	_, _, err := Parse(bytes.NewBuffer([]byte(`
route example.com
	header delete
`)))

	assert.ErrorContains(t, err, "invalid header delete line")
}

func Test_Parse_ErrorsOnHeaderReplaceWithoutValue(t *testing.T) {
	_, _, err := Parse(bytes.NewBuffer([]byte(`
route example.com
	header replace x-test
`)))

	assert.ErrorContains(t, err, "invalid header replace line")
}

func Test_Parse_ErrorsOnHeaderDefaultWithoutValue(t *testing.T) {
	_, _, err := Parse(bytes.NewBuffer([]byte(`
route example.com
	header default x-test
`)))

	assert.ErrorContains(t, err, "invalid header default line")
}

func Test_Parse_ErrorsOnMultipleProviders(t *testing.T) {
	_, _, err := Parse(bytes.NewBuffer([]byte(`
route example.com
	provider lego
	provider other
`)))

	assert.ErrorContains(t, err, "has multiple providers")
}

func Test_Parse_ReturnsRoutes(t *testing.T) {
	routes, _, err := Parse(bytes.NewBuffer([]byte(`
# Comment
route example.com www.example.com
	# Indented comment
	upstream localhost:8080
	header add x-test foo
	header delete x-test-2
	provider p1

route example.net
	upstream localhost:8081
	header default x-test-3 bar
	header replace x-test-4 baz
`)))

	assert.NoError(t, err)
	assert.Equal(t, 2, len(routes))
	assert.Equal(t, []string{"example.com", "www.example.com"}, routes[0].Domains)
	assert.Equal(t, []proxy.Upstream{{Host: "localhost:8080"}}, routes[0].Upstreams)
	assert.Equal(t, "p1", routes[0].Provider)
	assert.Equal(t, []string{"example.net"}, routes[1].Domains)
	assert.Equal(t, []proxy.Upstream{{Host: "localhost:8081"}}, routes[1].Upstreams)
	assert.Equal(t, "", routes[1].Provider)

	// Check headers for the first route
	assert.Equal(t, 2, len(routes[0].Headers))

	assert.Equal(t, "x-test", routes[0].Headers[0].Name)
	assert.Equal(t, "foo", routes[0].Headers[0].Value)
	assert.Equal(t, proxy.HeaderOpAdd, routes[0].Headers[0].Operation)

	assert.Equal(t, "x-test-2", routes[0].Headers[1].Name)
	assert.Equal(t, proxy.HeaderOpDelete, routes[0].Headers[1].Operation)

	// Check headers for the second route
	assert.Equal(t, 2, len(routes[1].Headers))

	assert.Equal(t, "x-test-3", routes[1].Headers[0].Name)
	assert.Equal(t, "bar", routes[1].Headers[0].Value)
	assert.Equal(t, proxy.HeaderOpDefault, routes[1].Headers[0].Operation)

	assert.Equal(t, "x-test-4", routes[1].Headers[1].Name)
	assert.Equal(t, "baz", routes[1].Headers[1].Value)
	assert.Equal(t, proxy.HeaderOpReplace, routes[1].Headers[1].Operation)
}

func Test_Parse_ParsesCaseInsensitively(t *testing.T) {
	routes, _, err := Parse(bytes.NewBuffer([]byte(`
# Comment
RoUtE example.com www.example.com
	# Indented comment
	UpStReAm localhost:8080
	HeAdEr AdD x-test foo
	hEaDeR dElEtE x-test-2
	PrOvIdEr p1

rOuTe example.net
	uPsTrEaM localhost:8081
	HeAdEr DeFaUlT x-test-3 bar
	hEaDeR rEpLaCe x-test-4 baz
`)))

	assert.NoError(t, err)
	assert.Equal(t, 2, len(routes))
	assert.Equal(t, []string{"example.com", "www.example.com"}, routes[0].Domains)
	assert.Equal(t, []proxy.Upstream{{Host: "localhost:8080"}}, routes[0].Upstreams)
	assert.Equal(t, "p1", routes[0].Provider)
	assert.Equal(t, []string{"example.net"}, routes[1].Domains)
	assert.Equal(t, []proxy.Upstream{{Host: "localhost:8081"}}, routes[1].Upstreams)

	// Check headers for the first route
	assert.Equal(t, 2, len(routes[0].Headers))

	assert.Equal(t, "x-test", routes[0].Headers[0].Name)
	assert.Equal(t, "foo", routes[0].Headers[0].Value)
	assert.Equal(t, proxy.HeaderOpAdd, routes[0].Headers[0].Operation)

	assert.Equal(t, "x-test-2", routes[0].Headers[1].Name)
	assert.Equal(t, proxy.HeaderOpDelete, routes[0].Headers[1].Operation)

	// Check headers for the second route
	assert.Equal(t, 2, len(routes[1].Headers))

	assert.Equal(t, "x-test-3", routes[1].Headers[0].Name)
	assert.Equal(t, "bar", routes[1].Headers[0].Value)
	assert.Equal(t, proxy.HeaderOpDefault, routes[1].Headers[0].Operation)

	assert.Equal(t, "x-test-4", routes[1].Headers[1].Name)
	assert.Equal(t, "baz", routes[1].Headers[1].Value)
	assert.Equal(t, proxy.HeaderOpReplace, routes[1].Headers[1].Operation)
}

func Test_Parse_MultipleUpstreams(t *testing.T) {
	routes, _, err := Parse(bytes.NewBuffer([]byte(`
route example.com www.example.com
	upstream localhost:8080
	upstream localhost:8081
	upstream localhost:8082

route example.net
	upstream localhost:8089
`)))

	assert.NoError(t, err)
	assert.Equal(t, 2, len(routes))

	assert.Equal(t, []proxy.Upstream{
		{Host: "localhost:8080"},
		{Host: "localhost:8081"},
		{Host: "localhost:8082"},
	}, routes[0].Upstreams)

	assert.Equal(t, []proxy.Upstream{
		{Host: "localhost:8089"},
	}, routes[1].Upstreams)
}

func Test_Parse_Fallback_SingleRoute(t *testing.T) {
	_, fallback, err := Parse(bytes.NewBuffer([]byte(`
route example.com www.example.com
	upstream localhost:8080
	fallback

route example.net
	upstream localhost:8089
`)))

	assert.NoError(t, err)
	assert.NotNil(t, fallback)

	assert.Equal(t, []proxy.Upstream{
		{Host: "localhost:8080"},
	}, fallback.Upstreams)

	assert.Equal(t, []string{"example.com", "www.example.com"}, fallback.Domains)
}

func Test_Parse_Fallback_OutsideRoute(t *testing.T) {
	_, _, err := Parse(bytes.NewBuffer([]byte(`fallback`)))

	assert.ErrorContains(t, err, "fallback without route")
}

func Test_Parse_Fallback_MultipleRoutes(t *testing.T) {
	_, _, err := Parse(bytes.NewBuffer([]byte(`
route example.com www.example.com
	upstream localhost:8080
	fallback

route example.net
	upstream localhost:8089
	fallback
`)))

	assert.ErrorContains(t, err, "multiple fallback routes specified")
}

func Test_Parse_RedirectToPrimary_OutsideRoute(t *testing.T) {
	_, _, err := Parse(bytes.NewBuffer([]byte(`redirect-to-primary`)))

	assert.ErrorContains(t, err, "redirect-to-primary without route")
}

func Test_Parse_RedirectToPrimary_MultipleDomains(t *testing.T) {
	routes, _, err := Parse(bytes.NewBuffer([]byte(`
route example.com www.example.com
	upstream localhost:8080
	redirect-to-primary
`)))

	assert.NoError(t, err)
	assert.NotNil(t, routes)

	assert.True(t, routes[0].RedirectToPrimary)
}

func Test_Parse_RedirectToPrimary_SingleDomain(t *testing.T) {
	_, _, err := Parse(bytes.NewBuffer([]byte(`
route example.com
	upstream localhost:8080
	redirect-to-primary
`)))

	assert.ErrorContains(t, err, "redirect-to-primary specified with only a single domain")
}

func Test_Parse_RedirectToPrimary_Repeated(t *testing.T) {
	_, _, err := Parse(bytes.NewBuffer([]byte(`
route example.com example.net
	redirect-to-primary
	upstream localhost:8080
	redirect-to-primary
`)))

	assert.ErrorContains(t, err, "multiple redirect-to-primary options specified")
}
