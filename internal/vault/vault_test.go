package vault

import (
	"bytes"
	"path/filepath"
	"sync"
	"testing"
)

func TestCredentialsAreBoundToOwnerAndTamperingFails(t *testing.T) {
	v, err := New(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	secret := []byte("test-only-credential")
	encrypted, err := v.Seal("alice:model:first", secret)
	if err != nil {
		t.Fatal(err)
	}
	second, _ := v.Seal("alice:model:first", secret)
	if bytes.Equal(encrypted, second) || bytes.Contains(encrypted, secret) {
		t.Fatal("credential was not randomly encrypted")
	}
	if _, err = v.Reveal("bob:model:first", encrypted); err == nil {
		t.Fatal("cross-owner disclosure")
	}
	if _, err = v.Reveal("alice:bot:first", encrypted); err == nil {
		t.Fatal("cross-purpose disclosure")
	}
	encrypted[len(encrypted)-1] ^= 1
	if _, err = v.Reveal("alice:model:first", encrypted); err == nil {
		t.Fatal("tampering accepted")
	}
}
func TestConcurrentAPIAndWorkerKeepOnePersistentKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.key")
	const count = 12
	opened := make([]*Vault, count)
	errs := make([]error, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) { defer wg.Done(); opened[i], errs[i] = Open("", path) }(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	encrypted, err := opened[0].Seal("owner", []byte("fixture"))
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range opened {
		plain, err := v.Reveal("owner", encrypted)
		if err != nil || string(plain) != "fixture" {
			t.Fatal("API/worker keys diverged", err)
		}
	}
	restarted, err := Open("", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = restarted.Reveal("owner", encrypted); err != nil {
		t.Fatal("restart lost credentials")
	}
}
