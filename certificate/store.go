package certificate

import (
	"encoding/json"
	"errors"
	"os"
)

// JsonStore is responsible for storing and managing certificates. It can save and load data to/from a JSON file.
type JsonStore struct {
	path string

	certificates []*Details
}

// NewStore creates a new certificate store using the specified path for storage, and tries to load any saved data.
func NewStore(path string) (*JsonStore, error) {
	j := &JsonStore{path: path}
	if err := j.load(); err != nil {
		return nil, err
	}
	return j, nil
}

// load attempts to load the current store from disk. If the file is not found, no error is returned.
func (j *JsonStore) load() error {
	b, err := os.ReadFile(j.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}

	return json.Unmarshal(b, &j.certificates)
}

// save serialises the current store to disk.
func (j *JsonStore) save() error {
	j.pruneCertificates()

	b, err := json.Marshal(j.certificates)
	if err != nil {
		return err
	}

	return os.WriteFile(j.path, b, 0600)
}

// GetCertificate returns a previously stored certificate with the given subject and alt names, or `nil` if none exists.
//
// Returned certificates are not guaranteed to be valid.
func (j *JsonStore) GetCertificate(subjectName string, altNames []string) *Details {
	for i := range j.certificates {
		if j.certificates[i].IsFor(subjectName, altNames) {
			return j.certificates[i]
		}
	}

	return nil
}

// removeCertificate removes any previously stored certificate with the given subject and alt names.
func (j *JsonStore) removeCertificate(subjectName string, altNames []string) {
	for i := range j.certificates {
		if j.certificates[i].IsFor(subjectName, altNames) {
			j.certificates = append(j.certificates[:i], j.certificates[i+1:]...)
			return
		}
	}
}

// pruneCertificates removes any certificates that are no longer valid.
func (j *JsonStore) pruneCertificates() {
	savedCerts := j.certificates[:0]
	for i := range j.certificates {
		if j.certificates[i].ValidFor(0) {
			savedCerts = append(savedCerts, j.certificates[i])
		}
	}
	j.certificates = savedCerts
}

// SaveCertificate adds the given certificate to the store. Any previously saved certificates for the same subject
// and alt names will be removed. The store will be saved to disk after the certificate is added.
func (j *JsonStore) SaveCertificate(certificate *Details) error {
	j.removeCertificate(certificate.Subject, certificate.AltNames)
	j.certificates = append(j.certificates, certificate)
	return j.save()
}
