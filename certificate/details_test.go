package certificate

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func Test_Details_ValidFor(t *testing.T) {
	tests := []struct {
		name     string
		notAfter time.Time
		period   time.Duration
		want     bool
	}{
		{"Valid for long period", time.Now().Add(time.Hour * 24 * 365 * 10), time.Hour, true},
		{"Valid for short period", time.Now().Add(time.Hour + time.Minute), time.Hour, true},
		{"Expired in the past", time.Now().Add(-time.Hour), time.Hour, false},
		{"Expires in the period", time.Now().Add(time.Minute * 30), time.Hour, false},
		{"Zero value time", time.Time{}, time.Hour, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Details{NotAfter: tt.notAfter}
			assert.Equal(t, tt.want, s.ValidFor(tt.period))
		})
	}
}

func Test_Details_HasStapleFor(t *testing.T) {
	tests := []struct {
		name       string
		nextUpdate time.Time
		period     time.Duration
		want       bool
	}{
		{"Valid for long period", time.Now().Add(time.Hour * 24 * 365 * 10), time.Hour, true},
		{"Valid for short period", time.Now().Add(time.Hour + time.Minute), time.Hour, true},
		{"Expired in the past", time.Now().Add(-time.Hour), time.Hour, false},
		{"Expires in the period", time.Now().Add(time.Minute * 30), time.Hour, false},
		{"Zero value time", time.Time{}, time.Hour, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Details{NextOcspUpdate: tt.nextUpdate}
			assert.Equal(t, tt.want, s.HasStapleFor(tt.period))
		})
	}
}

func Test_Details_IsFor(t *testing.T) {
	type args struct {
		subject  string
		altNames []string
	}
	tests := []struct {
		name       string
		certNames  args
		queryNames args
		want       bool
	}{
		{"Subject only, matches", args{subject: "example.com"}, args{subject: "example.com"}, true},
		{"Subject only, doesn't match", args{subject: "example.com"}, args{subject: "example.org"}, false},
		{"Subject and alt, matching", args{"example.com", []string{"example.org"}}, args{"example.com", []string{"example.org"}}, true},
		{"Subject and alt, swapped", args{"example.com", []string{"example.org"}}, args{"example.org", []string{"example.com"}}, false},
		{"Subject and alt, alt doesn't match", args{"example.com", []string{"example.org"}}, args{"example.com", []string{"example.net"}}, false},
		{"Subject and alt, checked against only subject", args{"example.com", []string{"example.org"}}, args{subject: "example.com"}, false},
		{"Subject only, checked against subject and alt", args{subject: "example.com"}, args{"example.com", []string{"example.org"}}, false},
		{"Extra alt in query", args{"example.com", []string{"example.org"}}, args{"example.com", []string{"example.org", "example.net"}}, false},
		{"Extra alt in cert", args{"example.com", []string{"example.org", "example.net"}}, args{"example.com", []string{"example.org"}}, false},
		{"Multiple alts, matching", args{"example.com", []string{"example.org", "example.net"}}, args{"example.com", []string{"example.org", "example.net"}}, true},
		{"Multiple alts, different order", args{"example.com", []string{"example.org", "example.net"}}, args{"example.com", []string{"example.net", "example.org"}}, true},
		{"Multiple alts, not matching", args{"example.com", []string{"example.org", "example.net"}}, args{"example.com", []string{"example.org", "example.xyz"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Details{
				Subject:  tt.certNames.subject,
				AltNames: tt.certNames.altNames,
			}
			assert.Equal(t, tt.want, s.IsFor(tt.queryNames.subject, tt.queryNames.altNames))
		})
	}
}
