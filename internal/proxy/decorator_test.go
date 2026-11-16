package proxy

import (
	"crypto/tls"
	"github.com/stretchr/testify/assert"
	"net"
	"net/http"
	"net/url"
	"testing"
)

func Test_bannedHeader(t *testing.T) {
	tests := []struct {
		name   string
		banned bool
	}{
		{"X-Real-IP", true},
		{"x-real-ip", true},
		{"X_Real_IP", true},
		{"Forwarded", true},
		{"True-Client-IP", true},
		{"CF-Connecting-IP", true},
		{"X-Original-URL", true},
		{"X-Forwarded-For", true},
		{"X-Forwarded-Port", true},
		{"x-forwarded-prefix", true},
		{"Tailscale-User-Login", true},
		{"Tailscale-App-Capabilities", true},
		{"Tailscale_User_Login", true},
		{"Some_Header", true},
		{"_", true},
		{"X-Real-IP-Info", false},
		{"Forwarded-Thing", false},
		{"X-Forwarded", false},
		{"Tailscale", false},
		{"Accept", false},
		{"Cookie", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.banned, bannedHeader(tt.name))
		})
	}
}

func Test_sanitiseHeaders(t *testing.T) {
	headers := http.Header{
		"X-Real-Ip":        {"1.2.3.4"},
		"X_real_ip":        {"1.2.3.4"},
		"X-Forwarded-Port": {"443"},
		"Accept":           {"text/html"},
		"Some-Header":      {"a", "b"},
	}

	removed := sanitiseHeaders(headers)

	assert.ElementsMatch(t, []string{"X-Real-Ip", "X_real_ip", "X-Forwarded-Port"}, removed)
	assert.Equal(t, http.Header{"Accept": {"text/html"}, "Some-Header": {"a", "b"}}, headers)
}

func Fuzz_sanitiseHeaders(f *testing.F) {
	f.Add("X-Real-IP", "Accept")
	f.Add("Tailscale_User_Login", "X-Forwarded-Port")
	f.Fuzz(
		func(t *testing.T, a, b string) {
			headers := http.Header{a: {"value"}, b: {"value"}, http.CanonicalHeaderKey(a): {"value"}}
			sanitiseHeaders(headers)
			for name := range headers {
				assert.False(t, bannedHeader(name), name)
			}
		},
	)
}

func Test_XForwardedForDecorator_passesOtherForwardedHeadersFromTrustedDownstreams(t *testing.T) {
	_, trustedNet, _ := net.ParseCIDR("192.168.1.0/24")
	decorator := &xForwardedForDecorator{trustedDownstreams: []net.IPNet{*trustedNet}}

	for _, remoteAddr := range []string{"192.168.1.10:5678", "10.0.0.1:5678"} {
		reqIn := &http.Request{
			Host:       "example.com",
			RemoteAddr: remoteAddr,
			Header: http.Header{
				"X-Forwarded-Port":   {"8443"},
				"X-Forwarded-Prefix": {"/a", "/b"},
				"X_forwarded_server": {"underscore"},
				"X-Forwarded-Server": {"hop"},
				"X-Forwarded-Host":   {"previous-hop.example.com"},
				"Connection":         {"keep-alive, X-Forwarded-Server, X-Forwarded-Host"},
				"X-Real-IP":          {"6.6.6.6"},
			},
		}
		reqOut := &http.Request{Host: "example.com", RemoteAddr: remoteAddr, Header: make(http.Header)}

		decorator.Decorate(reqIn, reqOut)

		if remoteAddr == "192.168.1.10:5678" {
			assert.Equal(t, []string{"8443"}, reqOut.Header.Values("X-Forwarded-Port"))
			assert.Equal(t, []string{"/a", "/b"}, reqOut.Header.Values("X-Forwarded-Prefix"))
		} else {
			assert.Empty(t, reqOut.Header.Values("X-Forwarded-Port"))
			assert.Empty(t, reqOut.Header.Values("X-Forwarded-Prefix"))
		}
		assert.Empty(t, reqOut.Header.Values("X-Forwarded-Server"))
		assert.Equal(t, "example.com", reqOut.Header.Get("X-Forwarded-Host"))
		assert.NotContains(t, reqOut.Header, "X_forwarded_server")
		assert.Empty(t, reqOut.Header.Values("X-Real-IP"))
	}
}

func Test_XForwardedForDecorator_addsIPv4(t *testing.T) {
	decorator := &xForwardedForDecorator{}

	u, _ := url.Parse("http://example.com")
	reqIn := &http.Request{
		URL:        u,
		RemoteAddr: "1.2.3.4:5678",
		Header:     make(http.Header),
	}
	reqOut := &http.Request{
		URL:        u,
		Host:       "example.com",
		RemoteAddr: "1.2.3.4:5678",
		Header:     make(http.Header),
	}

	decorator.Decorate(reqIn, reqOut)

	assert.Equal(t, "1.2.3.4", reqOut.Header.Get("X-Forwarded-For"))
}

func Test_XForwardedForDecorator_addsIPv6(t *testing.T) {
	decorator := &xForwardedForDecorator{}

	u, _ := url.Parse("http://example.com")
	reqIn := &http.Request{
		URL:        u,
		RemoteAddr: "[::1]:5678",
		Header:     make(http.Header),
	}
	reqOut := &http.Request{
		URL:        u,
		Host:       "example.com",
		RemoteAddr: "[::1]:5678",
		Header:     make(http.Header),
	}

	decorator.Decorate(reqIn, reqOut)

	assert.Equal(t, "::1", reqOut.Header.Get("X-Forwarded-For"))
}

func Test_XForwardedForDecorator_addsHttpsProtocol(t *testing.T) {
	decorator := &xForwardedForDecorator{}

	u, _ := url.Parse("/")
	reqIn := &http.Request{
		URL:        u,
		RemoteAddr: "[::1]:5678",
		Header:     make(http.Header),
		TLS:        &tls.ConnectionState{},
	}
	reqOut := &http.Request{
		URL:        u,
		Host:       "example.com",
		RemoteAddr: "[::1]:5678",
		Header:     make(http.Header),
		TLS:        &tls.ConnectionState{},
	}

	decorator.Decorate(reqIn, reqOut)

	assert.Equal(t, "https", reqOut.Header.Get("X-Forwarded-Proto"))
}

func Test_XForwardedForDecorator_addsHttpProtocol(t *testing.T) {
	decorator := &xForwardedForDecorator{}

	u, _ := url.Parse("/")
	reqIn := &http.Request{
		URL:        u,
		RemoteAddr: "[::1]:5678",
		Header:     make(http.Header),
		TLS:        nil,
	}
	reqOut := &http.Request{
		URL:        u,
		Host:       "example.com",
		RemoteAddr: "[::1]:5678",
		Header:     make(http.Header),
		TLS:        nil,
	}

	decorator.Decorate(reqIn, reqOut)

	assert.Equal(t, "http", reqOut.Header.Get("X-Forwarded-Proto"))
}

func Test_UserAgentDecorator_leavesExistingUA(t *testing.T) {
	decorator := &userAgentDecorator{}
	reqIn := &http.Request{
		Header: map[string][]string{
			"User-Agent": {"some-bot/1.0"},
		},
	}
	reqOut := &http.Request{
		Header: map[string][]string{
			"User-Agent": {"some-bot/1.0"},
		},
	}
	decorator.Decorate(reqIn, reqOut)

	assert.Equal(t, []string{"some-bot/1.0"}, reqOut.Header.Values("User-Agent"))
}

func Test_UserAgentDecorator_addsBlankIfUnset(t *testing.T) {
	decorator := &userAgentDecorator{}
	reqIn := &http.Request{
		Header: map[string][]string{},
	}
	reqOut := &http.Request{
		Header: map[string][]string{},
	}
	decorator.Decorate(reqIn, reqOut)

	assert.Equal(t, []string{""}, reqOut.Header.Values("User-Agent"))
}

func Test_XForwardedForDecorator_withTrustedDownstreams_preservesExistingHeaders(t *testing.T) {
	_, trustedNet, _ := net.ParseCIDR("192.168.1.0/24")
	decorator := &xForwardedForDecorator{
		trustedDownstreams: []net.IPNet{*trustedNet},
	}

	u, _ := url.Parse("http://example.com")
	reqIn := &http.Request{
		URL:        u,
		Host:       "example.com",
		RemoteAddr: "192.168.1.10:5678",
		Header: map[string][]string{
			"X-Forwarded-For":   {"10.0.0.1"},
			"X-Forwarded-Host":  {"original.example.com"},
			"X-Forwarded-Proto": {"https"},
		},
	}
	reqOut := &http.Request{
		URL:        u,
		Host:       "example.com",
		RemoteAddr: "192.168.1.10:5678",
		Header:     make(http.Header),
	}

	decorator.Decorate(reqIn, reqOut)

	assert.Equal(t, "10.0.0.1, 192.168.1.10", reqOut.Header.Get("X-Forwarded-For"))
	assert.Equal(t, "original.example.com", reqOut.Header.Get("X-Forwarded-Host"))
	assert.Equal(t, "https", reqOut.Header.Get("X-Forwarded-Proto"))
}

func Test_XForwardedForDecorator_withTrustedDownstreams_keepsEveryForwardedForLine(t *testing.T) {
	_, trustedNet, _ := net.ParseCIDR("192.168.1.0/24")
	decorator := &xForwardedForDecorator{trustedDownstreams: []net.IPNet{*trustedNet}}

	reqIn := &http.Request{
		Host:       "example.com",
		RemoteAddr: "192.168.1.10:5678",
		Header:     http.Header{"X-Forwarded-For": {"1.1.1.1", "2.2.2.2, 3.3.3.3"}},
	}
	reqOut := &http.Request{Host: "example.com", RemoteAddr: "192.168.1.10:5678", Header: make(http.Header)}

	decorator.Decorate(reqIn, reqOut)

	assert.Equal(t, []string{"1.1.1.1, 2.2.2.2, 3.3.3.3, 192.168.1.10"}, reqOut.Header.Values("X-Forwarded-For"))
}

func Test_XForwardedForDecorator_withTrustedDownstreams_replacesHeadersFromUntrustedSource(t *testing.T) {
	_, trustedNet, _ := net.ParseCIDR("192.168.1.0/24")
	decorator := &xForwardedForDecorator{
		trustedDownstreams: []net.IPNet{*trustedNet},
	}

	u, _ := url.Parse("http://example.com")
	reqIn := &http.Request{
		URL:        u,
		Host:       "example.com",
		RemoteAddr: "10.0.0.1:5678",
		Header: map[string][]string{
			"X-Forwarded-For":   {"192.168.1.10"},
			"X-Forwarded-Host":  {"malicious.example.com"},
			"X-Forwarded-Proto": {"https"},
		},
	}
	reqOut := &http.Request{
		URL:        u,
		Host:       "example.com",
		RemoteAddr: "10.0.0.1:5678",
		Header:     make(http.Header),
	}

	decorator.Decorate(reqIn, reqOut)

	assert.Equal(t, "10.0.0.1", reqOut.Header.Get("X-Forwarded-For"))
	assert.Equal(t, "example.com", reqOut.Header.Get("X-Forwarded-Host"))
	assert.Equal(t, "http", reqOut.Header.Get("X-Forwarded-Proto"))
}

func Test_XForwardedForDecorator_withTrustedDownstreams_setsHeadersWhenMissingFromTrustedSource(t *testing.T) {
	_, trustedNet, _ := net.ParseCIDR("192.168.1.0/24")
	decorator := &xForwardedForDecorator{
		trustedDownstreams: []net.IPNet{*trustedNet},
	}

	u, _ := url.Parse("http://example.com")
	reqIn := &http.Request{
		URL:        u,
		Host:       "example.com",
		RemoteAddr: "192.168.1.10:5678",
		Header:     make(http.Header),
	}
	reqOut := &http.Request{
		URL:        u,
		Host:       "example.com",
		RemoteAddr: "192.168.1.10:5678",
		Header:     make(http.Header),
	}

	decorator.Decorate(reqIn, reqOut)

	assert.Equal(t, "192.168.1.10", reqOut.Header.Get("X-Forwarded-For"))
	assert.Equal(t, "example.com", reqOut.Header.Get("X-Forwarded-Host"))
	assert.Equal(t, "http", reqOut.Header.Get("X-Forwarded-Proto"))
}

func Test_XForwardedForDecorator_withMultipleTrustedNetworks(t *testing.T) {
	_, trustedNet1, _ := net.ParseCIDR("192.168.1.0/24")
	_, trustedNet2, _ := net.ParseCIDR("10.0.0.0/8")
	decorator := &xForwardedForDecorator{
		trustedDownstreams: []net.IPNet{*trustedNet1, *trustedNet2},
	}

	tests := []struct {
		name          string
		remoteAddr    string
		expectedTrust bool
	}{
		{"Trusted network 1", "192.168.1.50:1234", true},
		{"Trusted network 2", "10.1.2.3:5678", true},
		{"Untrusted network", "203.0.113.1:9999", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, _ := url.Parse("http://example.com")
			reqIn := &http.Request{
				URL:        u,
				Host:       "example.com",
				RemoteAddr: tt.remoteAddr,
				Header: map[string][]string{
					"X-Forwarded-For": {"upstream.example.com"},
				},
			}
			reqOut := &http.Request{
				URL:        u,
				Host:       "example.com",
				RemoteAddr: tt.remoteAddr,
				Header:     make(http.Header),
			}

			decorator.Decorate(reqIn, reqOut)

			expectedIP, _, _ := net.SplitHostPort(tt.remoteAddr)
			if tt.expectedTrust {
				assert.Equal(t, "upstream.example.com, "+expectedIP, reqOut.Header.Get("X-Forwarded-For"))
			} else {
				assert.Equal(t, expectedIP, reqOut.Header.Get("X-Forwarded-For"))
			}
		})
	}
}

func Test_XForwardedForDecorator_withNoTrustedDownstreams_alwaysReplacesHeaders(t *testing.T) {
	decorator := &xForwardedForDecorator{
		trustedDownstreams: nil,
	}

	u, _ := url.Parse("http://example.com")
	reqIn := &http.Request{
		URL:        u,
		Host:       "example.com",
		RemoteAddr: "192.168.1.10:5678",
		Header: map[string][]string{
			"X-Forwarded-For":   {"10.0.0.1"},
			"X-Forwarded-Host":  {"original.example.com"},
			"X-Forwarded-Proto": {"https"},
		},
	}
	reqOut := &http.Request{
		URL:        u,
		Host:       "example.com",
		RemoteAddr: "192.168.1.10:5678",
		Header:     make(http.Header),
	}

	decorator.Decorate(reqIn, reqOut)

	assert.Equal(t, "192.168.1.10", reqOut.Header.Get("X-Forwarded-For"))
	assert.Equal(t, "example.com", reqOut.Header.Get("X-Forwarded-Host"))
	assert.Equal(t, "http", reqOut.Header.Get("X-Forwarded-Proto"))
}
