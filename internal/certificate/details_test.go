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

func Test_Details_ShouldRenew(t *testing.T) {
	tests := []struct {
		name            string
		notAfter        time.Time
		ariRenewalTime  time.Time
		minimumValidity time.Duration
		want            bool
	}{
		// No ARI - falls back to validity check
		{"No ARI, cert valid for period", time.Now().Add(time.Hour * 24), time.Time{}, time.Hour, false},
		{"No ARI, cert expires within period", time.Now().Add(time.Minute * 30), time.Time{}, time.Hour, true},
		{"No ARI, cert already expired", time.Now().Add(-time.Hour), time.Time{}, time.Hour, true},

		// With ARI - uses ARI renewal time
		{"ARI in future, cert valid", time.Now().Add(time.Hour * 24), time.Now().Add(time.Hour), time.Hour, false},
		{"ARI in past, cert valid", time.Now().Add(time.Hour * 24), time.Now().Add(-time.Hour), time.Hour, true},
		{"ARI in future, cert would expire", time.Now().Add(time.Minute * 30), time.Now().Add(time.Hour), time.Hour, false},
		{"ARI in past, cert already expired", time.Now().Add(-time.Hour), time.Now().Add(-time.Minute * 30), time.Hour, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Details{
				NotAfter:       tt.notAfter,
				AriRenewalTime: tt.ariRenewalTime,
			}
			assert.Equal(t, tt.want, s.ShouldRenew(tt.minimumValidity))
		})
	}
}

func Test_Details_RequiresStaple(t *testing.T) {
	cert := "-----BEGIN CERTIFICATE-----\nMIIDuDCCAz6gAwIBAgISA/GVWdX7eXUNyfsx+/kGdawlMAoGCCqGSM49BAMDMDIx\nCzAJBgNVBAYTAlVTMRYwFAYDVQQKEw1MZXQncyBFbmNyeXB0MQswCQYDVQQDEwJF\nNjAeFw0yNDEyMzEyMDExNTJaFw0yNTAzMzEyMDExNTFaMB4xHDAaBgNVBAMTE2Nv\nbnRhY3QuY2hhbWV0aC5jb20wdjAQBgcqhkjOPQIBBgUrgQQAIgNiAARascUGB0xf\n2aJMvSxpDw1afgymvDaByYAgHwC+m1rYmUoEFihqbKed7SvsKMjFT9F/1DQtNe3G\nbiijNtgC7vVrUfA7zajrSUnMo5Rh1v8YHhzV7NdIpszF19WRBHx3YNqjggIpMIIC\nJTAOBgNVHQ8BAf8EBAMCB4AwHQYDVR0lBBYwFAYIKwYBBQUHAwEGCCsGAQUFBwMC\nMAwGA1UdEwEB/wQCMAAwHQYDVR0OBBYEFFOC1EYV0Tb205nNhVotz+BnWnkPMB8G\nA1UdIwQYMBaAFJMnRpgDqVFojpjWxEJI2yO/WJTSMFUGCCsGAQUFBwEBBEkwRzAh\nBggrBgEFBQcwAYYVaHR0cDovL2U2Lm8ubGVuY3Iub3JnMCIGCCsGAQUFBzAChhZo\ndHRwOi8vZTYuaS5sZW5jci5vcmcvMB4GA1UdEQQXMBWCE2NvbnRhY3QuY2hhbWV0\naC5jb20wEwYDVR0gBAwwCjAIBgZngQwBAgEwggEFBgorBgEEAdZ5AgQCBIH2BIHz\nAPEAdgCi4wrkRe+9rZt+OO1HZ3dT14JbhJTXK14bLMS5UKRH5wAAAZQeji85AAAE\nAwBHMEUCIQCtpt1X49qC3Sr/hXLMW9OROwBzw5SbHCmWdNnkZa6s3AIgDbIZkxDd\nyaoQZk0aPI/oTNuNY9fMZzcswPw/Cx5S+scAdwDM+w9qhXEJZf6Vm1PO6bJ8IumF\nXA2XjbapflTA/kwNsAAAAZQeji9HAAAEAwBIMEYCIQDrSdM1yABkxJr+b87FQL7N\nTXJIuUmKl0mhWf4iUFUcKQIhAOkcRKmLta6goBR/eNm1xbDmwcldIAtY/UBckMwK\n4J6dMBEGCCsGAQUFBwEYBAUwAwIBBTAKBggqhkjOPQQDAwNoADBlAjEA1Dxzma2E\n8IpQUVT8X42xmcas9WISwLlO7DxN45QNbANnMMXK+/4dKi7cwQY5bX9jAjAhfvjk\nPe1I7vYVaRVBvI0STQ24CMfT8LPd7YJuHVrX2eVWZciUircG0Sg151aKB2g=\n-----END CERTIFICATE-----"
	details := Details{Certificate: cert}
	assert.True(t, details.RequiresStaple())
}
