// Package secret keeps credentials in the operating system's credential store.
package secret

import (
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

var ErrNotFound = errors.New("secret not found")

type Store struct{ Service string }

func (s Store) Set(account, value string) error {
	if err := keyring.Set(s.Service, account, value); err != nil {
		return fmt.Errorf("OS keyring: %w", err)
	}
	return nil
}

func (s Store) Get(account string) (string, error) {
	value, err := keyring.Get(s.Service, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("OS keyring: %w", err)
	}
	return value, nil
}

func (s Store) Delete(account string) error {
	err := keyring.Delete(s.Service, account)
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("OS keyring: %w", err)
	}
	return nil
}
