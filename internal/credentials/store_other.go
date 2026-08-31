//go:build !windows

package credentials

import "errors"

type unsupportedStore struct{}

func newPlatformStore() secretStore {
	return unsupportedStore{}
}

func (unsupportedStore) Read(string) (string, bool, error) {
	return "", false, errors.New("WINDOWS_CREDENTIAL_MANAGER_REQUIRED")
}

func (unsupportedStore) Write(string, string) error {
	return errors.New("WINDOWS_CREDENTIAL_MANAGER_REQUIRED")
}

func (unsupportedStore) Delete(string) error {
	return errors.New("WINDOWS_CREDENTIAL_MANAGER_REQUIRED")
}
