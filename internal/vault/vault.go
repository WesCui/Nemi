// Package vault protects credentials at rest and binds ciphertext to its owner.
package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
)

type Vault struct{ aead cipher.AEAD }

func New(key []byte) (*Vault, error) {
	if len(key) != 32 {
		return nil, errors.New("credential encryption key must contain 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	return &Vault{aead: aead}, err
}

// Open uses an explicit deployment key or creates a local key atomically. The
// same path must be mounted into the API and worker; never regenerate a lost key.
func Open(keyHex, path string) (*Vault, error) {
	if keyHex != "" {
		key, err := hex.DecodeString(keyHex)
		if err != nil {
			return nil, errors.New("invalid credential encryption key")
		}
		return New(key)
	}
	key, err := os.ReadFile(path)
	if err == nil {
		return New(key)
	}
	if !os.IsNotExist(err) {
		return nil, errors.New("credential key unavailable")
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, errors.New("credential key directory unavailable")
	}
	key = make([]byte, 32)
	if _, err = rand.Read(key); err != nil {
		return nil, err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".key-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(key)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	// Hard-link publishes a complete file without overwriting another process's key.
	if err = os.Link(f.Name(), path); err != nil && !os.IsExist(err) {
		return nil, errors.New("cannot publish credential key")
	}
	key, err = os.ReadFile(path)
	if err != nil {
		return nil, errors.New("credential key unavailable")
	}
	return New(key)
}

func (v *Vault) Seal(owner string, plain []byte) ([]byte, error) {
	if v == nil {
		return nil, errors.New("credential vault unavailable")
	}
	nonce := make([]byte, v.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return v.aead.Seal(nonce, nonce, plain, []byte(owner)), nil
}
func (v *Vault) Reveal(owner string, encrypted []byte) ([]byte, error) {
	if v == nil || len(encrypted) < v.aead.NonceSize() {
		return nil, errors.New("credential unavailable")
	}
	n := v.aead.NonceSize()
	plain, err := v.aead.Open(nil, encrypted[:n], encrypted[n:], []byte(owner))
	if err != nil {
		return nil, errors.New("credential unavailable")
	}
	return plain, nil
}
