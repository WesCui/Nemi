package files

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"nemi/internal/config"
	"nemi/internal/domain"
	"nemi/internal/store"
	"nemi/internal/vault"
)

type Service struct {
	Store   *store.Store
	Vault   *vault.Vault
	Objects Objects
	Storage string
	Parse   func(context.Context, string, []byte) (domain.FileContent, error)
}
type envelope struct {
	Data    []byte             `json:"data"`
	Content domain.FileContent `json:"content"`
}

func New(c config.Config, st *store.Store, v *vault.Vault) (*Service, error) {
	o, kind, err := objects(c)
	if err != nil {
		return nil, err
	}
	return &Service{st, v, o, kind, ParseProcess}, nil
}
func (s *Service) Save(ctx context.Context, tx pgx.Tx, w string, ref *store.Ref, name, kind, url string, data []byte, content domain.FileContent) (domain.File, error) {
	var f domain.File
	mime, err := MIME(name)
	if err != nil {
		return f, err
	}
	if len(data) == 0 || len(data) > MaxBytes {
		return f, errors.New("FILE_TOO_LARGE")
	}
	if err = Validate(content); err != nil {
		return f, err
	}
	if err = s.Store.AdmitFile(ctx, tx, w, ref, int64(len(data))); err != nil {
		return f, err
	}
	f = domain.File{ID: domain.ID(), Name: name, Kind: kind, MIME: mime, Size: int64(len(data)), OriginURL: url, CreatedAt: time.Now().UTC()}
	hash := sha256.Sum256([]byte(w))
	key := hex.EncodeToString(hash[:]) + "/" + f.ID
	plain, _ := json.Marshal(envelope{data, content})
	encrypted, err := s.Vault.Seal(w+":file:"+f.ID, plain)
	if err != nil {
		return domain.File{}, errors.New("FILE_STORAGE_UNAVAILABLE")
	}
	putCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if s.Objects.Put(putCtx, key, encrypted) != nil {
		return domain.File{}, errors.New("FILE_STORAGE_UNAVAILABLE")
	}
	// On uncertain DB commit retain the immutable object. Never delete a blob
	// that a committed row may reference. Orphan cleanup is an operator task.
	err = s.Store.InsertFile(ctx, tx, w, store.FileRecord{File: f, Storage: s.Storage, ObjectKey: key}, ref)
	return f, err
}
func (s *Service) Read(ctx context.Context, w string, f store.FileRecord) ([]byte, domain.FileContent, error) {
	if f.Storage != s.Storage {
		return nil, domain.FileContent{}, errors.New("FILE_STORAGE_UNAVAILABLE")
	}
	readCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	cipher, err := s.Objects.Get(readCtx, f.ObjectKey)
	if err != nil {
		return nil, domain.FileContent{}, errors.New("FILE_STORAGE_UNAVAILABLE")
	}
	plain, err := s.Vault.Reveal(w+":file:"+f.ID, cipher)
	if err != nil {
		return nil, domain.FileContent{}, errors.New("FILE_STORAGE_UNAVAILABLE")
	}
	var e envelope
	if json.Unmarshal(plain, &e) != nil || len(e.Data) > MaxBytes || Validate(e.Content) != nil {
		return nil, domain.FileContent{}, errors.New("FILE_STORAGE_UNAVAILABLE")
	}
	return e.Data, e.Content, nil
}
