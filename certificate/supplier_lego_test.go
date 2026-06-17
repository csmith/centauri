package certificate

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-acme/lego/v5/acme"
	"github.com/go-acme/lego/v5/acme/api"
	"github.com/go-acme/lego/v5/certcrypto"
	legocert "github.com/go-acme/lego/v5/certificate"
	"github.com/go-acme/lego/v5/registration"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ocsp"
)

func Test_acmeUser_load_generatesKeyIfFileIsMissing(t *testing.T) {
	user := &acmeUser{}
	require.NoError(t, user.load(filepath.Join(t.TempDir(), "user.json")))
	assert.NotNil(t, user.GetPrivateKey())
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
}

type fakeRegistrar struct {
	res     *acme.ExtendedAccount
	err     error
	opts    registration.RegisterOptions
	eabOpts registration.RegisterEABOptions
}

func (f *fakeRegistrar) Register(ctx context.Context, options registration.RegisterOptions) (*acme.ExtendedAccount, error) {
	f.opts = options
	return f.res, f.err
}

func (f *fakeRegistrar) RegisterWithExternalAccountBinding(ctx context.Context, options registration.RegisterEABOptions) (*acme.ExtendedAccount, error) {
	f.eabOpts = options
	return f.res, f.err
}

func Test_acmeUser_registerAndSave_errorsIfRegistrarErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "user.json")
	r := &fakeRegistrar{
		err: fmt.Errorf("denied"),
	}
	privateKey, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	user := &acmeUser{key: privateKey}
	assert.Error(t, user.registerAndSave(t.Context(), r, path))
	assert.True(t, r.opts.TermsOfServiceAgreed)
}

func Test_acmeUser_registerAndSave_updatesRegistrationResource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "user.json")
	r := &fakeRegistrar{
		res: &acme.ExtendedAccount{Location: "https://example.com/acme/reg/1"},
	}
	privateKey, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	user := &acmeUser{key: privateKey}
	require.NoError(t, user.registerAndSave(t.Context(), r, path))
	assert.NotNil(t, user.account)
	assert.Equal(t, r.res.Location, user.GetRegistration().Location)
}

func Test_acmeUser_registerAndSave_writesDetailsToDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "user.json")
	r := &fakeRegistrar{
		res: &acme.ExtendedAccount{Location: "https://example.com/acme/reg/1"},
	}

	privateKey, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	user := &acmeUser{
		email: "test@example.com",
		key:   privateKey,
	}
	require.NoError(t, user.registerAndSave(t.Context(), r, path))

	newUser := &acmeUser{}
	require.NoError(t, newUser.load(path))
	assert.Equal(t, user, newUser)
}

func Test_acmeUser_registerAndSave_callsRegisterWithEabBindingWhenEabCredentialsConfigured(t *testing.T) {
	path := filepath.Join(t.TempDir(), "user.json")
	r := &fakeRegistrar{
		res: &acme.ExtendedAccount{Location: "https://example.com/acme/reg/1"},
	}
	privateKey, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	user := &acmeUser{
		key:     privateKey,
		eabKid:  "kid",
		eabHmac: "hmac",
	}
	require.NoError(t, user.registerAndSave(t.Context(), r, path))
	assert.NotNil(t, user.account)
	assert.True(t, r.eabOpts.TermsOfServiceAgreed)
	assert.Equal(t, "kid", r.eabOpts.Kid)
	assert.Equal(t, "hmac", r.eabOpts.HmacEncoded)
	assert.Empty(t, r.opts.TermsOfServiceAgreed)
}

func Test_acmeUser_registerAndSave_returnsErrorIfEabRegisterFailsWithEabCredentialsConfigured(t *testing.T) {
	path := filepath.Join(t.TempDir(), "user.json")
	r := &fakeRegistrar{
		err: fmt.Errorf("denied"),
	}
	privateKey, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	user := &acmeUser{
		key:     privateKey,
		eabKid:  "kid",
		eabHmac: "hmac",
	}
	assert.Error(t, user.registerAndSave(t.Context(), r, path))
	assert.True(t, r.eabOpts.TermsOfServiceAgreed)
	assert.Equal(t, "kid", r.eabOpts.Kid)
	assert.Empty(t, r.opts.TermsOfServiceAgreed)
}

type fakeCertifier struct {
	request         legocert.ObtainRequest
	resource        *legocert.Resource
	bundle          []byte
	rawResponse     []byte
	response        *ocsp.Response
	ocspErr         error
	obtainErr       error
	renewalInfoCert *x509.Certificate
	renewalInfoRes  *legocert.RenewalInfo
	renewalInfoErr  error
}

func (f *fakeCertifier) GetOCSP(ctx context.Context, bundle []byte) ([]byte, *ocsp.Response, error) {
	f.bundle = bundle
	return f.rawResponse, f.response, f.ocspErr
}

func (f *fakeCertifier) Obtain(ctx context.Context, request legocert.ObtainRequest) (*legocert.Resource, error) {
	f.request = request
	return f.resource, f.obtainErr
}

func (f *fakeCertifier) GetRenewalInfo(ctx context.Context, cert *x509.Certificate) (*legocert.RenewalInfo, error) {
	f.renewalInfoCert = cert
	return f.renewalInfoRes, f.renewalInfoErr
}

func Test_Supplier_GetCertificate_passesDetailsToCertifier(t *testing.T) {
	c := &fakeCertifier{
		obtainErr: fmt.Errorf("denied"),
	}
	s := &LegoSupplier{
		certifier: c,
		keyType:   certcrypto.EC384,
	}

	_, _ = s.GetCertificate(t.Context(), "example.com", []string{"alt.example.com", "example.net"}, true)
	assert.Equal(t, c.request.Domains, []string{"example.com", "alt.example.com", "example.net"})
	assert.True(t, c.request.Bundle)
	assert.True(t, c.request.MustStaple)
	assert.Equal(t, certcrypto.EC384, c.request.KeyType)
}

func Test_Supplier_GetCertificate_passesProfileToCertifier(t *testing.T) {
	c := &fakeCertifier{
		obtainErr: fmt.Errorf("denied"),
	}
	s := &LegoSupplier{
		certifier: c,
		profile:   "shortlived",
		keyType:   certcrypto.EC384,
	}

	_, _ = s.GetCertificate(t.Context(), "example.com", nil, false)
	assert.Equal(t, "shortlived", c.request.Profile)
}

func Test_Supplier_GetCertificate_returnsErrorIfObtainFails(t *testing.T) {
	c := &fakeCertifier{
		obtainErr: fmt.Errorf("denied"),
	}
	s := &LegoSupplier{
		certifier: c,
	}

	_, err := s.GetCertificate(t.Context(), "example.com", []string{"alt.example.com", "example.net"}, true)
	assert.Error(t, err)
}

func Test_Supplier_GetCertificate_returnsErrorIfCertificateCantBeParsed(t *testing.T) {
	c := &fakeCertifier{
		resource: &legocert.Resource{Certificate: []byte("not a pem")},
	}
	s := &LegoSupplier{
		certifier: c,
	}

	_, err := s.GetCertificate(t.Context(), "example.com", []string{"alt.example.com", "example.net"}, true)
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

	_, err := s.GetCertificate(t.Context(), "example.com", []string{"alt.example.com", "example.net"}, true)
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

	cert, err := s.GetCertificate(t.Context(), "example.com", []string{"alt.example.com", "example.net"}, false)
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

	cert, err := s.GetCertificate(t.Context(), "example.com", []string{"alt.example.com", "example.net"}, true)
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
	assert.Error(t, s.UpdateStaple(t.Context(), &Details{Certificate: "cert"}))
}

func Test_Supplier_UpdateStaple_errorsIfStaplerReturnsNil(t *testing.T) {
	s := &LegoSupplier{
		certifier: &fakeCertifier{
			response: nil,
		},
	}
	assert.Error(t, s.UpdateStaple(t.Context(), &Details{Certificate: "cert"}))
}

func Test_Supplier_UpdateStaple_errorsIfStaplerReturnStatusOtherTHanGood(t *testing.T) {
	s := &LegoSupplier{
		certifier: &fakeCertifier{
			response: &ocsp.Response{Status: ocsp.Revoked},
		},
	}
	assert.Error(t, s.UpdateStaple(t.Context(), &Details{Certificate: "cert"}))
}

func Test_Supplier_UpdateStaple_passesCertToStapler(t *testing.T) {
	c := &fakeCertifier{}
	s := &LegoSupplier{
		certifier: c,
	}
	_ = s.UpdateStaple(t.Context(), &Details{Certificate: "cert"})
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
	require.NoError(t, s.UpdateStaple(t.Context(), cert))
	assert.Equal(t, c.response.NextUpdate, cert.NextOcspUpdate)
	assert.Equal(t, c.rawResponse, cert.OcspResponse)
}

func Test_Supplier_UpdateRenewalInfo_errorsIfCertificateCantBeParsed(t *testing.T) {
	s := &LegoSupplier{
		certifier: &fakeCertifier{},
	}
	err := s.UpdateRenewalInfo(t.Context(), &Details{Certificate: "not a pem"})
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
	err := s.UpdateRenewalInfo(t.Context(), cert)
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

	err := s.UpdateRenewalInfo(t.Context(), &Details{Certificate: string(pemCert)})
	assert.Error(t, err)
}

func Test_Supplier_UpdateRenewalInfo_passesCertToCertifier(t *testing.T) {
	privateKey, _ := certcrypto.GeneratePrivateKey(certcrypto.RSA2048)
	pemCert, _ := certcrypto.GeneratePemCert(privateKey.(*rsa.PrivateKey), "example.com", nil)
	expectedCert, _ := certcrypto.ParsePEMCertificate(pemCert)

	c := &fakeCertifier{
		renewalInfoRes: &legocert.RenewalInfo{
			ExtendedRenewalInfo: &acme.ExtendedRenewalInfo{
				RenewalInfo: acme.RenewalInfo{
					SuggestedWindow: acme.Window{
						Start: time.Now().Add(time.Hour),
						End:   time.Now().Add(time.Hour * 2),
					},
				},
			},
		},
	}
	s := &LegoSupplier{
		certifier: c,
	}

	_ = s.UpdateRenewalInfo(t.Context(), &Details{Certificate: string(pemCert)})
	assert.Equal(t, expectedCert, c.renewalInfoCert)
}

func Test_Supplier_UpdateRenewalInfo_updatesARIDetails(t *testing.T) {
	privateKey, _ := certcrypto.GeneratePrivateKey(certcrypto.RSA2048)
	pemCert, _ := certcrypto.GeneratePemCert(privateKey.(*rsa.PrivateKey), "example.com", nil)

	windowStart := time.Now().Add(time.Hour)
	windowEnd := time.Now().Add(time.Hour * 2)

	c := &fakeCertifier{
		renewalInfoRes: &legocert.RenewalInfo{
			ExtendedRenewalInfo: &acme.ExtendedRenewalInfo{
				RenewalInfo: acme.RenewalInfo{
					SuggestedWindow: acme.Window{
						Start: windowStart,
						End:   windowEnd,
					},
					ExplanationURL: "https://example.com/explanation",
				},
				RetryAfter: time.Hour * 6,
			},
		},
	}
	s := &LegoSupplier{
		certifier: c,
	}

	cert := &Details{Certificate: string(pemCert)}
	before := time.Now()
	require.NoError(t, s.UpdateRenewalInfo(t.Context(), cert))

	assert.Equal(t, "https://example.com/explanation", cert.AriExplanation)
	assert.True(t, cert.AriNextUpdate.After(before.Add(time.Hour*6-time.Second)), "AriNextUpdate should be approximately now + RetryAfter")
	assert.True(t, cert.AriNextUpdate.Before(before.Add(time.Hour*6+time.Second)), "AriNextUpdate should be approximately now + RetryAfter")
	assert.True(t, !cert.AriRenewalTime.Before(windowStart), "AriRenewalTime should be >= window start")
	assert.True(t, cert.AriRenewalTime.Before(windowEnd), "AriRenewalTime should be < window end")
}
