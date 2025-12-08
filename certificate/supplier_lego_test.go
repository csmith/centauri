package certificate

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-acme/lego/v4/acme"
	"github.com/go-acme/lego/v4/acme/api"
	"github.com/go-acme/lego/v4/certcrypto"
	legocert "github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/registration"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ocsp"
)

func Test_acmeUser_load_generatesKeyIfFileIsMissing(t *testing.T) {
	user := &acmeUser{}
	require.NoError(t, user.load(filepath.Join(t.TempDir(), "user.json")))
	assert.NotNil(t, user.GetPrivateKey())
	assert.NotEmptyf(t, user.Key, "expected key to be serialised")
}

func Test_acmeUser_load_errorsIfFileIsUnreadable(t *testing.T) {
	user := &acmeUser{}
	err := user.load(t.TempDir())
	assert.Error(t, err)
}

func Test_acmeUser_load_errorsIfFileCantBeUnmarshalled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "user.json")
	_ = os.WriteFile(path, []byte("{invalid json"), 0600)

	user := &acmeUser{}
	err := user.load(path)
	assert.Error(t, err)
}

func Test_acmeUser_load_errorsIfKeyCantBeParsed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "user.json")
	_ = os.WriteFile(path, []byte(`{"key": "nope"}`), 0600)

	user := &acmeUser{}
	err := user.load(path)
	assert.Error(t, err)
}

func Test_acmeUser_load_restoresSavedKey(t *testing.T) {
	privateKey, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	encoded := certcrypto.PEMEncode(privateKey)
	marshalled, _ := json.Marshal(string(encoded))

	path := filepath.Join(t.TempDir(), "user.json")
	_ = os.WriteFile(path, []byte(`{"key": `+string(marshalled)+`}`), 0600)

	user := &acmeUser{}
	err := user.load(path)
	require.NoError(t, err)

	assert.Equal(t, privateKey, user.key)
	assert.EqualValues(t, encoded, user.Key)
}

type fakeRegistrar struct {
	res  *registration.Resource
	err  error
	opts registration.RegisterOptions
}

func (f *fakeRegistrar) Register(options registration.RegisterOptions) (*registration.Resource, error) {
	f.opts = options
	return f.res, f.err
}

func Test_acmeUser_registerAndSave_errorsIfRegistrarErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "user.json")
	r := &fakeRegistrar{
		err: fmt.Errorf("denied"),
	}
	user := &acmeUser{}
	assert.Error(t, user.registerAndSave(r, path))
	assert.True(t, r.opts.TermsOfServiceAgreed)
}

func Test_acmeUser_registerAndSave_updatesRegistrationResource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "user.json")
	r := &fakeRegistrar{
		res: &registration.Resource{URI: "https://example.com/acme/reg/1"},
	}
	user := &acmeUser{}
	require.NoError(t, user.registerAndSave(r, path))
	assert.NotNil(t, user.Registration)
	assert.Equal(t, r.res.URI, user.GetRegistration().URI)
}

func Test_acmeUser_registerAndSave_writesDetailsToDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "user.json")
	r := &fakeRegistrar{
		res: &registration.Resource{URI: "https://example.com/acme/reg/1"},
	}

	privateKey, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	encoded := certcrypto.PEMEncode(privateKey)
	user := &acmeUser{
		Email: "test@example.com",
		key:   privateKey,
		Key:   string(encoded),
	}
	require.NoError(t, user.registerAndSave(r, path))

	newUser := &acmeUser{}
	require.NoError(t, newUser.load(path))
	assert.Equal(t, user, newUser)
}

type fakeCertifier struct {
	request          legocert.ObtainRequest
	resource         *legocert.Resource
	bundle           []byte
	rawResponse      []byte
	response         *ocsp.Response
	ocspErr          error
	obtainErr        error
	renewalInfoReq   legocert.RenewalInfoRequest
	renewalInfoRes   *legocert.RenewalInfoResponse
	renewalInfoErr   error
}

func (f *fakeCertifier) GetOCSP(bundle []byte) ([]byte, *ocsp.Response, error) {
	f.bundle = bundle
	return f.rawResponse, f.response, f.ocspErr
}

func (f *fakeCertifier) Obtain(request legocert.ObtainRequest) (*legocert.Resource, error) {
	f.request = request
	return f.resource, f.obtainErr
}

func (f *fakeCertifier) GetRenewalInfo(req legocert.RenewalInfoRequest) (*legocert.RenewalInfoResponse, error) {
	f.renewalInfoReq = req
	return f.renewalInfoRes, f.renewalInfoErr
}

func Test_Supplier_GetCertificate_passesDetailsToCertifier(t *testing.T) {
	c := &fakeCertifier{
		obtainErr: fmt.Errorf("denied"),
	}
	s := &LegoSupplier{
		certifier: c,
	}

	_, _ = s.GetCertificate("example.com", []string{"alt.example.com", "example.net"}, true)
	assert.Equal(t, c.request.Domains, []string{"example.com", "alt.example.com", "example.net"})
	assert.True(t, c.request.Bundle)
	assert.True(t, c.request.MustStaple)
}

func Test_Supplier_GetCertificate_passesProfileToCertifier(t *testing.T) {
	c := &fakeCertifier{
		obtainErr: fmt.Errorf("denied"),
	}
	s := &LegoSupplier{
		certifier: c,
		profile:   "shortlived",
	}

	_, _ = s.GetCertificate("example.com", nil, false)
	assert.Equal(t, "shortlived", c.request.Profile)
}

func Test_Supplier_GetCertificate_returnsErrorIfObtainFails(t *testing.T) {
	c := &fakeCertifier{
		obtainErr: fmt.Errorf("denied"),
	}
	s := &LegoSupplier{
		certifier: c,
	}

	_, err := s.GetCertificate("example.com", []string{"alt.example.com", "example.net"}, true)
	assert.Error(t, err)
}

func Test_Supplier_GetCertificate_returnsErrorIfCertificateCantBeParsed(t *testing.T) {
	c := &fakeCertifier{
		resource: &legocert.Resource{Certificate: []byte("not a pem")},
	}
	s := &LegoSupplier{
		certifier: c,
	}

	_, err := s.GetCertificate("example.com", []string{"alt.example.com", "example.net"}, true)
	assert.Error(t, err)
}

func Test_Supplier_GetCertificate_returnsErrorIfStaplingFailsWhenEnabled(t *testing.T) {
	privateKey, _ := certcrypto.GeneratePrivateKey(certcrypto.RSA2048)
	pemCert, _ := certcrypto.GeneratePemCert(privateKey.(*rsa.PrivateKey), "example.com", nil)

	c := &fakeCertifier{
		resource: &legocert.Resource{Certificate: pemCert},
		ocspErr:  fmt.Errorf("denied"),
	}
	s := &LegoSupplier{
		certifier: c,
	}

	_, err := s.GetCertificate("example.com", []string{"alt.example.com", "example.net"}, true)
	assert.Error(t, err)
}

func Test_Supplier_GetCertificate_doesNotTryToStapleIfDisabled(t *testing.T) {
	privateKey, _ := certcrypto.GeneratePrivateKey(certcrypto.RSA2048)
	pemCert, _ := certcrypto.GeneratePemCert(privateKey.(*rsa.PrivateKey), "example.com", nil)

	c := &fakeCertifier{
		resource: &legocert.Resource{Certificate: pemCert},
		ocspErr:  fmt.Errorf("denied"),
	}
	s := &LegoSupplier{
		certifier: c,
	}

	cert, err := s.GetCertificate("example.com", []string{"alt.example.com", "example.net"}, false)
	assert.NoError(t, err)
	assert.EqualValues(t, cert.Certificate, pemCert)
}

func Test_Supplier_GetCertificate_returnsCertificateDetails(t *testing.T) {
	privateKey, _ := certcrypto.GeneratePrivateKey(certcrypto.RSA2048)
	pemCert, _ := certcrypto.GeneratePemCert(privateKey.(*rsa.PrivateKey), "example.com", nil)
	realCert, _ := certcrypto.ParsePEMCertificate(pemCert)

	c := &fakeCertifier{
		resource: &legocert.Resource{
			Certificate:       pemCert,
			PrivateKey:        []byte("private key"),
			IssuerCertificate: []byte("issuer"),
		},
		rawResponse: []byte("raw"),
		response: &ocsp.Response{
			Status:     ocsp.Good,
			NextUpdate: time.Now().Add(time.Hour),
		},
	}
	s := &LegoSupplier{
		certifier: c,
	}

	cert, err := s.GetCertificate("example.com", []string{"alt.example.com", "example.net"}, true)
	assert.NoError(t, err)
	assert.EqualValues(t, cert.Certificate, pemCert)
	assert.Equal(t, cert.Issuer, "issuer")
	assert.Equal(t, cert.PrivateKey, "private key")
	assert.Equal(t, cert.Subject, "example.com")
	assert.Equal(t, cert.AltNames, []string{"alt.example.com", "example.net"})
	assert.Equal(t, cert.NotAfter, realCert.NotAfter)
	assert.EqualValues(t, cert.OcspResponse, "raw")
	assert.Equal(t, cert.NextOcspUpdate, c.response.NextUpdate)
}

func Test_Supplier_UpdateStaple_errorsIfStaplerErrors(t *testing.T) {
	s := &LegoSupplier{
		certifier: &fakeCertifier{
			ocspErr: fmt.Errorf("denied"),
		},
	}
	assert.Error(t, s.UpdateStaple(&Details{Certificate: "cert"}))
}

func Test_Supplier_UpdateStaple_errorsIfStaplerReturnsNil(t *testing.T) {
	s := &LegoSupplier{
		certifier: &fakeCertifier{
			response: nil,
		},
	}
	assert.Error(t, s.UpdateStaple(&Details{Certificate: "cert"}))
}

func Test_Supplier_UpdateStaple_errorsIfStaplerReturnStatusOtherTHanGood(t *testing.T) {
	s := &LegoSupplier{
		certifier: &fakeCertifier{
			response: &ocsp.Response{Status: ocsp.Revoked},
		},
	}
	assert.Error(t, s.UpdateStaple(&Details{Certificate: "cert"}))
}

func Test_Supplier_UpdateStaple_passesCertToStapler(t *testing.T) {
	c := &fakeCertifier{}
	s := &LegoSupplier{
		certifier: c,
	}
	_ = s.UpdateStaple(&Details{Certificate: "cert"})
	assert.EqualValues(t, "cert", c.bundle)
}

func Test_Supplier_UpdateStaple_updatesOcspDetails(t *testing.T) {
	c := &fakeCertifier{
		rawResponse: []byte("raw"),
		response: &ocsp.Response{
			Status:     ocsp.Good,
			NextUpdate: time.Now().Add(time.Hour),
		},
	}
	s := &LegoSupplier{
		certifier: c,
	}

	cert := &Details{Certificate: "cert"}
	require.NoError(t, s.UpdateStaple(cert))
	assert.Equal(t, c.response.NextUpdate, cert.NextOcspUpdate)
	assert.Equal(t, c.rawResponse, cert.OcspResponse)
}

func Test_Supplier_UpdateRenewalInfo_errorsIfCertificateCantBeParsed(t *testing.T) {
	s := &LegoSupplier{
		certifier: &fakeCertifier{},
	}
	err := s.UpdateRenewalInfo(&Details{Certificate: "not a pem"})
	assert.Error(t, err)
}

func Test_Supplier_UpdateRenewalInfo_returnsNilIfServerDoesNotSupportARI(t *testing.T) {
	privateKey, _ := certcrypto.GeneratePrivateKey(certcrypto.RSA2048)
	pemCert, _ := certcrypto.GeneratePemCert(privateKey.(*rsa.PrivateKey), "example.com", nil)

	c := &fakeCertifier{
		renewalInfoErr: api.ErrNoARI,
	}
	s := &LegoSupplier{
		certifier: c,
	}

	cert := &Details{Certificate: string(pemCert)}
	err := s.UpdateRenewalInfo(cert)
	assert.NoError(t, err)
	assert.True(t, cert.AriRenewalTime.IsZero(), "should not set renewal time when ARI not supported")
}

func Test_Supplier_UpdateRenewalInfo_returnsErrorIfGetRenewalInfoFails(t *testing.T) {
	privateKey, _ := certcrypto.GeneratePrivateKey(certcrypto.RSA2048)
	pemCert, _ := certcrypto.GeneratePemCert(privateKey.(*rsa.PrivateKey), "example.com", nil)

	c := &fakeCertifier{
		renewalInfoErr: fmt.Errorf("some other error"),
	}
	s := &LegoSupplier{
		certifier: c,
	}

	err := s.UpdateRenewalInfo(&Details{Certificate: string(pemCert)})
	assert.Error(t, err)
}

func Test_Supplier_UpdateRenewalInfo_passesCertToCertifier(t *testing.T) {
	privateKey, _ := certcrypto.GeneratePrivateKey(certcrypto.RSA2048)
	pemCert, _ := certcrypto.GeneratePemCert(privateKey.(*rsa.PrivateKey), "example.com", nil)
	expectedCert, _ := certcrypto.ParsePEMCertificate(pemCert)

	c := &fakeCertifier{
		renewalInfoRes: &legocert.RenewalInfoResponse{
			RenewalInfoResponse: acme.RenewalInfoResponse{
				SuggestedWindow: acme.Window{
					Start: time.Now().Add(time.Hour),
					End:   time.Now().Add(time.Hour * 2),
				},
			},
			RetryAfter: time.Hour,
		},
	}
	s := &LegoSupplier{
		certifier: c,
	}

	_ = s.UpdateRenewalInfo(&Details{Certificate: string(pemCert)})
	assert.Equal(t, expectedCert, c.renewalInfoReq.Cert)
}

func Test_Supplier_UpdateRenewalInfo_updatesARIDetails(t *testing.T) {
	privateKey, _ := certcrypto.GeneratePrivateKey(certcrypto.RSA2048)
	pemCert, _ := certcrypto.GeneratePemCert(privateKey.(*rsa.PrivateKey), "example.com", nil)

	windowStart := time.Now().Add(time.Hour)
	windowEnd := time.Now().Add(time.Hour * 2)

	c := &fakeCertifier{
		renewalInfoRes: &legocert.RenewalInfoResponse{
			RenewalInfoResponse: acme.RenewalInfoResponse{
				SuggestedWindow: acme.Window{
					Start: windowStart,
					End:   windowEnd,
				},
				ExplanationURL: "https://example.com/explanation",
			},
			RetryAfter: time.Hour * 6,
		},
	}
	s := &LegoSupplier{
		certifier: c,
	}

	cert := &Details{Certificate: string(pemCert)}
	before := time.Now()
	require.NoError(t, s.UpdateRenewalInfo(cert))

	assert.Equal(t, "https://example.com/explanation", cert.AriExplanation)
	assert.True(t, cert.AriNextUpdate.After(before.Add(time.Hour*6-time.Second)), "AriNextUpdate should be approximately now + RetryAfter")
	assert.True(t, cert.AriNextUpdate.Before(before.Add(time.Hour*6+time.Second)), "AriNextUpdate should be approximately now + RetryAfter")
	assert.True(t, !cert.AriRenewalTime.Before(windowStart), "AriRenewalTime should be >= window start")
	assert.True(t, cert.AriRenewalTime.Before(windowEnd), "AriRenewalTime should be < window end")
}
