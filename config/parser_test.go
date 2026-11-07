package config

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"strings"
	"testing"

	"github.com/csmith/centauri/proxy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_Parse_ReturnsEmptySliceForEmptyFile(t *testing.T) {
	routes, _, err := Parse(bytes.NewBuffer([]byte("")))

	assert.NoError(t, err)
	assert.Equal(t, 0, len(routes))
}

func Test_Parse_ErrorsOnUnknownLine(t *testing.T) {
	_, _, err := Parse(bytes.NewBuffer([]byte("error please")))

	assert.ErrorContains(t, err, "line 1: invalid line")
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

	assert.ErrorContains(t, err, "line 3: invalid header operation")
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

func Test_Parse_IgnoresExtraWhitespaceBetweenDomains(t *testing.T) {
	routes, _, err := Parse(strings.NewReader("route example.com  www.example.com \t example.net\n\tupstream app\n"))

	require.NoError(t, err)
	assert.Equal(t, []string{"example.com", "www.example.com", "example.net"}, routes[0].Domains)
}

func Test_Parse_RemovesTrailingDotsFromDomains(t *testing.T) {
	routes, _, err := Parse(strings.NewReader("route Example.com. www.example.com\n\tupstream app\n"))

	require.NoError(t, err)
	assert.Equal(t, []string{"example.com", "www.example.com"}, routes[0].Domains)
}

func Test_Parse_ErrorsOnInvalidRouteDomains(t *testing.T) {
	tests := []string{
		"*.example.com",
		"example..com",
		"example.com..",
		".",
		"example-.com",
		"example.com:443",
		"example.com/path",
		"127.0.0.1",
		"[::1]",
		"bücher.example",
	}
	for _, domain := range tests {
		t.Run(
			domain,
			func(t *testing.T) {
				_, _, err := Parse(
					strings.NewReader("route example.net\n\tupstream app\nroute example.com " + domain + "\n\tupstream app\n"),
				)
				assert.ErrorContains(t, err, "line 3: invalid domain name")
			},
		)
	}
}

func Test_Parse_RouteDomainsAreAcceptedByManager(t *testing.T) {
	routes, fallback, err := Parse(
		strings.NewReader("route Example.com. www.example.com  _srv.example.com\n\tupstream app\n\tfallback\n"),
	)
	require.NoError(t, err)

	assert.NoError(t, proxy.NewManager(nil).SetRoutes(t.Context(), routes, fallback))
}

func Test_Parse_ErrorsOnIntermediateRouteWithoutUpstreams(t *testing.T) {
	_, _, err := Parse(bytes.NewBuffer([]byte(`
route example.com
route example.net
	upstream localhost:8080
`)))

	assert.ErrorContains(t, err, "line 2: no upstreams specified for route")
}

func Test_Parse_ErrorsOnFinalRouteWithoutUpstreams(t *testing.T) {
	_, _, err := Parse(bytes.NewBuffer([]byte(`
route example.org
	upstream localhost:8080

route example.com
	provider lego
`)))

	assert.ErrorContains(t, err, "line 5: no upstreams specified for route")
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

func Test_Parse_LowercasesDomains(t *testing.T) {
	routes, _, err := Parse(
		bytes.NewBufferString("route Example.com WWW.example.COM\n\tupstream localhost:8080\n\tredirect-to-primary\n"),
	)

	require.NoError(t, err)
	assert.Equal(t, []string{"example.com", "www.example.com"}, routes[0].Domains)
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

func Test_Parse_Subject_OutsideRoute(t *testing.T) {
	_, _, err := Parse(bytes.NewBuffer([]byte(`subject foo`)))

	assert.ErrorContains(t, err, "subject without route")
}

func Test_Parse_Subject_Empty(t *testing.T) {
	_, _, err := Parse(bytes.NewBuffer([]byte(`
route example.com
	upstream localhost:8080
	subject
`)))

	assert.ErrorContains(t, err, "no domains specified for subject")
}

func Test_Parse_Subject_Single(t *testing.T) {
	routes, _, err := Parse(bytes.NewBuffer([]byte(`
route example.com
	upstream localhost:8080
	subject foo
`)))

	assert.NoError(t, err)
	assert.Equal(t, []string{"foo"}, routes[0].Subject)
}

func Test_Parse_Subject_MultipleOnOneLine(t *testing.T) {
	routes, _, err := Parse(bytes.NewBuffer([]byte(`
route example.com
	upstream localhost:8080
	subject foo bar baz
`)))

	assert.NoError(t, err)
	assert.Equal(t, []string{"foo", "bar", "baz"}, routes[0].Subject)
}

func Test_Parse_Subject_IgnoresExtraSpaces(t *testing.T) {
	routes, _, err := Parse(strings.NewReader("route example.com\n\tupstream localhost:8080\n\tsubject foo  bar\n"))

	require.NoError(t, err)
	assert.Equal(t, []string{"foo", "bar"}, routes[0].Subject)
}

func Test_Parse_Subject_MultipleLines(t *testing.T) {
	routes, _, err := Parse(bytes.NewBuffer([]byte(`
route example.com
	upstream localhost:8080
	subject foo
	subject bar
`)))

	assert.NoError(t, err)
	assert.Equal(t, []string{"foo", "bar"}, routes[0].Subject)
}

func Test_Parse_OnError_UpstreamOnly(t *testing.T) {
	routes, _, err := Parse(bytes.NewBuffer([]byte(`
route example.com
	upstream localhost:8080
	on_error 502 error-pages:8080
`)))

	assert.NoError(t, err)
	assert.Equal(t, []proxy.ErrorMapping{{Status: 502, Upstream: "error-pages:8080"}}, routes[0].ErrorMappings)
}

func Test_Parse_OnError_WithPath(t *testing.T) {
	routes, _, err := Parse(bytes.NewBuffer([]byte(`
route example.com
	upstream localhost:8080
	on_error 404 not-found:8080/not-found
`)))

	assert.NoError(t, err)
	assert.Equal(t, []proxy.ErrorMapping{{Status: 404, Upstream: "not-found:8080", Path: "/not-found"}}, routes[0].ErrorMappings)
}

func Test_Parse_OnError_WithQuery(t *testing.T) {
	routes, _, err := Parse(bytes.NewBuffer([]byte(`
route example.com
	upstream localhost:8080
	on_error 500 error-pages:8080/error?code=500
`)))

	assert.NoError(t, err)
	assert.Equal(t, []proxy.ErrorMapping{{Status: 500, Upstream: "error-pages:8080", Path: "/error", RawQuery: "code=500"}}, routes[0].ErrorMappings)
}

func Test_Parse_OnError_TrailingSlashIsTreatedAsNoPath(t *testing.T) {
	routes, _, err := Parse(bytes.NewBuffer([]byte(`
route example.com
	upstream localhost:8080
	on_error 502 error-pages:8080/
`)))

	assert.NoError(t, err)
	assert.Equal(t, []proxy.ErrorMapping{{Status: 502, Upstream: "error-pages:8080"}}, routes[0].ErrorMappings)
}

func Test_Parse_OnError_MultipleDirectives(t *testing.T) {
	routes, _, err := Parse(bytes.NewBuffer([]byte(`
route example.com
	upstream localhost:8080
	on_error 404 not-found:8080
	on_error 502 error-pages:8080
`)))

	assert.NoError(t, err)
	assert.Equal(t, []proxy.ErrorMapping{
		{Status: 404, Upstream: "not-found:8080"},
		{Status: 502, Upstream: "error-pages:8080"},
	}, routes[0].ErrorMappings)
}

func Test_Parse_OnError_OutsideRoute(t *testing.T) {
	_, _, err := Parse(bytes.NewBuffer([]byte(`on_error 502 error-pages:8080`)))

	assert.ErrorContains(t, err, "on_error without route")
}

func Test_Parse_OnError_TooFewParameters(t *testing.T) {
	_, _, err := Parse(bytes.NewBuffer([]byte(`
route example.com
	upstream localhost:8080
	on_error 502
`)))

	assert.ErrorContains(t, err, "invalid on_error line")
}

func Test_Parse_OnError_NonNumericStatus(t *testing.T) {
	_, _, err := Parse(bytes.NewBuffer([]byte(`
route example.com
	upstream localhost:8080
	on_error unicorn error-pages:8080
`)))

	assert.ErrorContains(t, err, "invalid status code")
}

func Test_Parse_OnError_StatusTooLow(t *testing.T) {
	_, _, err := Parse(bytes.NewBuffer([]byte(`
route example.com
	upstream localhost:8080
	on_error 302 error-pages:8080
`)))

	assert.ErrorContains(t, err, "invalid status code")
}

func Test_Parse_OnError_StatusTooHigh(t *testing.T) {
	_, _, err := Parse(bytes.NewBuffer([]byte(`
route example.com
	upstream localhost:8080
	on_error 600 error-pages:8080
`)))

	assert.ErrorContains(t, err, "invalid status code")
}

func Test_Parse_OnError_QueryWithoutPath(t *testing.T) {
	_, _, err := Parse(bytes.NewBuffer([]byte(`
route example.com
	upstream localhost:8080
	on_error 502 error-pages:8080?code=502
`)))

	assert.ErrorContains(t, err, "path must begin with a /")
}

func Test_Parse_NormalisesUpstreamAddresses(t *testing.T) {
	tests := []struct {
		upstream string
		expected string
	}{
		{"app", "app:80"},
		{"App.Internal.:8080", "app.internal:8080"},
		{"my_app-1:08080", "my_app-1:8080"},
		{"192.168.1.1:9000", "192.168.1.1:9000"},
		{"[::1]", "[::1]:80"},
		{"[0:0::1]:9000", "[::1]:9000"},
		{"Bücher.internal:8080", "xn--bcher-kva.internal:8080"},
	}
	for _, tt := range tests {
		t.Run(
			tt.upstream,
			func(t *testing.T) {
				routes, _, err := Parse(
					strings.NewReader("route example.com\n\tupstream " + tt.upstream + "\n\ton_error 502 " + tt.upstream + "/error\n"),
				)
				require.NoError(t, err)
				assert.Equal(t, []proxy.Upstream{{Host: tt.expected}}, routes[0].Upstreams)
				assert.Equal(t, tt.expected, routes[0].ErrorMappings[0].Upstream)
			},
		)
	}
}

func Test_Parse_ErrorsOnInvalidUpstreamAddresses(t *testing.T) {
	tests := []string{
		"http://app",
		"user@app",
		"app:0",
		"app:99999",
		"app:-1",
		"app:",
		"app:http",
		"app server",
		"app..internal",
		"::1",
		"[app]:80",
		"[192.168.1.1]:80",
		"[::1",
		"bad host",
	}
	for _, upstream := range tests {
		t.Run(
			upstream,
			func(t *testing.T) {
				_, _, err := Parse(strings.NewReader("route example.com\n\tupstream " + upstream + "\n"))
				require.ErrorContains(t, err, "line 2: invalid")

				_, _, err = Parse(strings.NewReader("route example.com\n\tupstream app\n\ton_error 502 " + upstream + "\n"))
				require.ErrorContains(t, err, "line 3: invalid")
			},
		)
	}
}

func Test_Parse_ErrorsOnUpstreamWithPath(t *testing.T) {
	_, _, err := Parse(strings.NewReader("route example.com\n\tupstream app/path\n"))

	assert.ErrorContains(t, err, "line 2: invalid")
}

func Test_Parse_NonASCIIUpstreamIsAllowedByDialler(t *testing.T) {
	routes, _, err := Parse(strings.NewReader("route example.com\n\tupstream bücher.internal:8080\n"))
	require.NoError(t, err)

	manager := proxy.NewManager(nil)
	require.NoError(t, manager.SetRoutes(t.Context(), routes, nil))

	var routed *http.Request
	proxy.NewRouter(
		manager,
		http.HandlerFunc(
			func(_ http.ResponseWriter, request *http.Request) {
				routed = request
			},
		),
		nil,
	).ServeHTTP(
		httptest.NewRecorder(),
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://example.com/", nil),
	)
	require.NotNil(t, routed)

	proxyRequest := &httputil.ProxyRequest{In: routed, Out: routed.Clone(routed.Context())}
	proxy.NewRewriter(nil, nil).RewriteRequest(proxyRequest)

	errNoDNS := errors.New("no dns in tests")
	blocked := false
	dialer := &proxy.AllowlistDialer{
		Dialer: net.Dialer{
			Resolver: &net.Resolver{
				PreferGo: true,
				Dial: func(context.Context, string, string) (net.Conn, error) {
					return nil, errNoDNS
				},
			},
		},
		OnBlock: func() { blocked = true },
	}

	response, err := (&http.Transport{DialContext: dialer.DialContext}).RoundTrip(proxyRequest.Out)
	if response != nil {
		_ = response.Body.Close()
	}
	require.ErrorContains(t, err, errNoDNS.Error())
	require.NotErrorIs(t, err, proxy.ErrDialNotAllowed)
	assert.False(t, blocked)
}
