// Package secret keeps credentials in the OS keyring (Windows Credential Manager, macOS Keychain, Secret Service).
package secret

import (
	"errors"
	"fmt"
	"strings"

	"github.com/zalando/go-keyring"
)

// ErrNotFound means the keyring has no entry for the account.
var ErrNotFound = errors.New("secret not found")

// Service is the shared keyring service used by all workctl providers.
const Service = "workctl"

// Account returns the stable account name for a provider profile.
func Account(kind, profile string) string {
	return strings.TrimSpace(kind) + ":" + strings.TrimSpace(profile)
}

// Store is the set of keyring entries of one service name.
type Store struct{ Service string }

// Set stores value for the account, replacing an existing entry.
func (s Store) Set(account, value string) error {
	if err := keyring.Set(s.Service, account, value); err != nil {
		return fmt.Errorf("OS keyring: %w", err)
	}
	return nil
}

// Get returns the value stored for the account, or ErrNotFound.
func (s Store) Get(account string) (string, error) {
	v, err := keyring.Get(s.Service, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("OS keyring: %w", err)
	}
	return v, nil
}

// Delete removes the entry; a missing entry is not an error.
func (s Store) Delete(account string) error {
	err := keyring.Delete(s.Service, account)
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("OS keyring: %w", err)
	}
	return nil
}
