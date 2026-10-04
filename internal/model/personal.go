package model

import (
	"context"
	"errors"
	"nemi/internal/config"
	"nemi/internal/store"
	"nemi/internal/vault"
)

func CredentialOwner(workspace, id string) string { return workspace + ":model:" + id }
func Personal(m store.PersonalModel, w string, v *vault.Vault) (*Gateway, error) {
	key, err := v.Reveal(CredentialOwner(w, m.ID), m.Credential)
	if err != nil {
		return nil, errors.New("MODEL_CREDENTIAL_UNAVAILABLE")
	}
	if _, ok := ProviderByID(m.Provider); !ok {
		return nil, errors.New("MODEL_PROVIDER_UNSUPPORTED")
	}
	g := New(config.Config{Provider: m.Provider, Model: m.Model, Key: string(key), InputPrice: m.InputPrice, OutputPrice: m.OutputPrice})
	g.ConfigID = m.ID
	return g, nil
}
func Resolve(ctx context.Context, s *store.Store, v *vault.Vault, fallback *Gateway, r store.Ref) (*Gateway, error) {
	m, err := s.RunModel(ctx, r)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return fallback, nil
	}
	g, err := Personal(*m, r.Workspace, v)
	if err == nil {
		g.HTTP = fallback.HTTP
	}
	return g, err
}
