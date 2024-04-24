package proxy

import (
	"crypto/tls"
	"github.com/stretchr/testify/assert"
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
			req := &http.Request{Header: tt.given}
			decorator.Decorate(req)
			assert.Equal(t, tt.expected, req.Header)
		})
	}
}

func Test_XForwardedForDecorator_addsIPv4(t *testing.T) {
	decorator := &xForwardedForDecorator{}

	u, _ := url.Parse("http://example.com")
	req := &http.Request{
		URL:        u,
		RemoteAddr: "1.2.3.4:5678",
		Header:     make(http.Header),
	}

	decorator.Decorate(req)

	assert.Equal(t, "1.2.3.4", req.Header.Get("X-Forwarded-For"))
}

func Test_XForwardedForDecorator_addsIPv6(t *testing.T) {
	decorator := &xForwardedForDecorator{}

	u, _ := url.Parse("http://example.com")
	req := &http.Request{
		URL:        u,
		RemoteAddr: "[::1]:5678",
		Header:     make(http.Header),
	}

	decorator.Decorate(req)

	assert.Equal(t, "::1", req.Header.Get("X-Forwarded-For"))
}

func Test_XForwardedForDecorator_addsHttpsProtocol(t *testing.T) {
	decorator := &xForwardedForDecorator{}

	u, _ := url.Parse("/")
	req := &http.Request{
		URL:        u,
		RemoteAddr: "[::1]:5678",
		Header:     make(http.Header),
		TLS:        &tls.ConnectionState{},
	}

	decorator.Decorate(req)

	assert.Equal(t, "https", req.Header.Get("X-Forwarded-Proto"))
}

func Test_XForwardedForDecorator_addsHttpProtocol(t *testing.T) {
	decorator := &xForwardedForDecorator{}

	u, _ := url.Parse("/")
	req := &http.Request{
		URL:        u,
		RemoteAddr: "[::1]:5678",
		Header:     make(http.Header),
		TLS:        nil,
	}

	decorator.Decorate(req)

	assert.Equal(t, "http", req.Header.Get("X-Forwarded-Proto"))
}

func Test_UserAgentDecorator_leavesExistingUA(t *testing.T) {
	decorator := &userAgentDecorator{}
	req := &http.Request{
		Header: map[string][]string{
			"User-Agent": {"some-bot/1.0"},
		},
	}
	decorator.Decorate(req)

	assert.Equal(t, []string{"some-bot/1.0"}, req.Header.Values("User-Agent"))
}

func Test_UserAgentDecorator_addsBlankIfUnset(t *testing.T) {
	decorator := &userAgentDecorator{}
	req := &http.Request{
		Header: map[string][]string{},
	}
	decorator.Decorate(req)

	assert.Equal(t, []string{""}, req.Header.Values("User-Agent"))
}
