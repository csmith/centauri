package proxy

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_isDomainName(t *testing.T) {
	tests := []struct {
		domain string
		want   bool
	}{
		{"example.com", true},
		{"example", true},
		{"test.example.com", true},
		{"example.com:8080", false},
		{"example=.com", false},
		{"example.com/foo/", false},
		{"example-.com", false},
		{"example..com", false},
		{"example.com with spaces", false},
		{".com", false},
		{"invalid-because-this-part-is-just-longer-than-sixty-four-characters.example.com", false},
		{strings.Repeat("invalid-because-the-overall-host-is-too-long.", 6) + ".example.com", false},
		{"127.0.0.1", false},
		{"127.0.0.1:8080", false},
		{"::1", false},
		{"[::1]:8080", false},
	}

	for _, tt := range tests {
		t.Run(tt.domain, func(t *testing.T) {
			assert.Equalf(t, tt.want, isDomainName(tt.domain), "isDomainName(%v)", tt.domain)
		})
	}
}
