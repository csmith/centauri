package proxy

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

type fakeResponseWriter struct {
	header     http.Header
	statusCode int
}

func (f *fakeResponseWriter) Header() http.Header {
	return f.header
}

func (f *fakeResponseWriter) Write(bytes []byte) (int, error) {
	// Don't care
	return 0, nil
}

func (f *fakeResponseWriter) WriteHeader(statusCode int) {
	f.statusCode = statusCode
}

func Test_Redirector_ErrorsIfHostIsEmpty(t *testing.T) {
	u, _ := url.Parse("/foo/bar")
	request := &http.Request{
		URL:        u,
		Header:     make(http.Header),
		RemoteAddr: "127.0.0.1:11003",
		Host:       "",
	}

	writer := &fakeResponseWriter{
		header: make(http.Header),
	}

	redirector := &Redirector{}
	redirector.ServeHTTP(writer, request)

	assert.Equal(t, http.StatusBadRequest, writer.statusCode)
}

func Test_Redirector_ErrorsIfHostIsInvalid(t *testing.T) {
	tests := []string{
		"/invalid/",
		"invalid with spaces",
		"127.0.0.1",
		"127.0.0.1:80",
		"[::1]",
		"[::1]:80",
		"invalid-.example.com",
		"invalid.-example.com",
		"invalid..example.com",
		"invalid.example.com-",
		"invalid-because-this-part-is-just-longer-than-sixty-four-characters.example.com",
		strings.Repeat("invalid-because-the-overall-host-is-too-long.", 6) + ".example.com",
	}

	for _, test := range tests {
		t.Run(test, func(t *testing.T) {
			u, _ := url.Parse("/foo/bar")
			request := &http.Request{
				URL:        u,
				Header:     make(http.Header),
				RemoteAddr: "127.0.0.1:11003",
				Host:       test,
			}

			writer := &fakeResponseWriter{
				header: make(http.Header),
			}

			redirector := &Redirector{}
			redirector.ServeHTTP(writer, request)

			assert.Equal(t, http.StatusBadRequest, writer.statusCode)
		})
	}
}

func Test_Redirector_RedirectsToHttpsUrl(t *testing.T) {
	u, _ := url.Parse("/foo/bar")
	request := &http.Request{
		URL:        u,
		Header:     make(http.Header),
		RemoteAddr: "127.0.0.1:11003",
		Host:       "example.com",
	}

	writer := &fakeResponseWriter{
		header: make(http.Header),
	}

	redirector := &Redirector{}
	redirector.ServeHTTP(writer, request)

	assert.Equal(t, http.StatusPermanentRedirect, writer.statusCode)
	assert.Equal(t, "https://example.com/foo/bar", writer.header.Get("Location"))
}

func Test_Redirector_PreservesQueryString(t *testing.T) {
	u, _ := url.Parse("/foo/bar?baz=quux")
	request := &http.Request{
		URL:        u,
		Header:     make(http.Header),
		RemoteAddr: "127.0.0.1:11003",
		Host:       "example.com",
	}

	writer := &fakeResponseWriter{
		header: make(http.Header),
	}

	redirector := &Redirector{}
	redirector.ServeHTTP(writer, request)

	assert.Equal(t, http.StatusPermanentRedirect, writer.statusCode)
	assert.Equal(t, "https://example.com/foo/bar?baz=quux", writer.header.Get("Location"))
}

func Test_Redirector_StripsPort(t *testing.T) {
	u, _ := url.Parse("/foo/bar")
	request := &http.Request{
		URL:        u,
		Header:     make(http.Header),
		RemoteAddr: "127.0.0.1:11003",
		Host:       "example.com:80",
	}

	writer := &fakeResponseWriter{
		header: make(http.Header),
	}

	redirector := &Redirector{}
	redirector.ServeHTTP(writer, request)

	assert.Equal(t, http.StatusPermanentRedirect, writer.statusCode)
	assert.Equal(t, "https://example.com/foo/bar", writer.header.Get("Location"))
}
