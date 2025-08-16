package proxy

import (
	"crypto/tls"
	"github.com/stretchr/testify/assert"
	"net"
	"net/http"
	"net/url"
	"testing"
)

func Test_BannedHeaderDecorator_removesHeaders(t *testing.T) {
	decorator := bannedHeaderDecorator{
		headers: []string{
			"x-test-1",
			"x-test-2",
		},
	}

	tests := []struct {
		name     string
		given    http.Header
		expected http.Header
	}{
		{
			name:     "No headers",
			given:    map[string][]string{},
			expected: map[string][]string{},
		},
		{
			name: "No matching headers",
			given: map[string][]string{
				"Some-Other-Header": {"Foo", "Bar"},
			},
			expected: map[string][]string{
				"Some-Other-Header": {"Foo", "Bar"},
			},
		},
		{
			name: "Multiple values",
			given: map[string][]string{
				"X-Test-1": {"Foo", "Bar"},
			},
			expected: map[string][]string{},
		},
		{
			name: "Multiple headers",
			given: map[string][]string{
				"X-Test-1": {"Foo", "Bar"},
				"X-Test-2": {"Baz"},
				"X-Test-3": {"Quux"},
			},
			expected: map[string][]string{
				"X-Test-3": {"Quux"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqIn := &http.Request{Header: tt.given}
			reqOut := &http.Request{Header: make(http.Header)}
			for k, v := range tt.given {
				reqOut.Header[k] = v
			}
			decorator.Decorate(reqIn, reqOut)
			assert.Equal(t, tt.expected, reqOut.Header)
		})
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
