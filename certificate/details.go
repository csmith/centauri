package certificate

import (
	"sort"
	"time"
)

// Details contains the details of a certificate we've previously obtained and saved for future use.
type Details struct {
	Issuer      string `json:"issuer"`
	PrivateKey  string `json:"privateKey"`
	Certificate string `json:"certificate"`

	Subject  string    `json:"subject"`
	AltNames []string  `json:"altNames"`
	NotAfter time.Time `json:"notAfter"`

	OcspResponse   []byte    `json:"ocspResponse"`
	NextOcspUpdate time.Time `json:"nextOcspUpdate"`
}

// ValidFor indicates whether the certificate will be valid for the entirety of the given period.
func (s *Details) ValidFor(period time.Duration) bool {
	return s.NotAfter.After(time.Now().Add(period))
}

// HasStapleFor indicates whether the OCSP staple covers the entirety of the given period.
func (s *Details) HasStapleFor(period time.Duration) bool {
	return s.NextOcspUpdate.After(time.Now().Add(period))
}

// IsFor determines whether this certificate covers the given subject and altNames (and no more).
func (s *Details) IsFor(subject string, altNames []string) bool {
	if s.Subject != subject || len(s.AltNames) != len(altNames) {
		return false
	}

	// Create copies of the names, so we can in-place sort them without mutating random caller data.
	altNames1 := append([]string(nil), s.AltNames...)
	altNames2 := append([]string(nil), altNames...)
	sort.Strings(altNames1)
	sort.Strings(altNames2)

	for i := range altNames1 {
		if altNames1[i] != altNames2[i] {
			return false
		}
	}

	return true
}
