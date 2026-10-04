package store

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"nemi/internal/domain"
	"time"
)

type PersonalModel struct {
	ID          string     `json:"id"`
	Label       string     `json:"label"`
	Provider    string     `json:"provider"`
	Model       string     `json:"model"`
	InputPrice  int64      `json:"input_price_micro_cny"`
	OutputPrice int64      `json:"output_price_micro_cny"`
	VerifiedAt  *time.Time `json:"verified_at"`
	Credential  []byte     `json:"-"`
}

const modelColumns = "id,label,provider,model,input_price,output_price,verified_at,credential"

func readPersonalModel(row pgx.Row) (PersonalModel, error) {
	var m PersonalModel
	err := row.Scan(&m.ID, &m.Label, &m.Provider, &m.Model, &m.InputPrice, &m.OutputPrice, &m.VerifiedAt, &m.Credential)
	return m, err
}
func (s *Store) Models(ctx context.Context, w string) ([]PersonalModel, string, error) {
	list := []PersonalModel{}
	rows, err := s.Pool.Query(ctx, "SELECT "+modelColumns+" FROM personal_models WHERE workspace_id=$1 AND NOT revoked ORDER BY created_at", w)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	for rows.Next() {
		m, e := readPersonalModel(rows)
		if e != nil {
			return nil, "", e
		}
		list = append(list, m)
	}
	if err = rows.Err(); err != nil {
		return nil, "", err
	}
	var selected string
	err = s.Pool.QueryRow(ctx, "SELECT model_id FROM workspace_model_settings WHERE workspace_id=$1", w).Scan(&selected)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	return list, selected, err
}
func (s *Store) AddModel(ctx context.Context, tx pgx.Tx, w string, m PersonalModel) (any, int, error) {
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,4))", w); err != nil {
		return nil, 0, err
	}
	var count int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM personal_models WHERE workspace_id=$1 AND NOT revoked", w).Scan(&count); err != nil {
		return nil, 0, err
	}
	if count >= 12 {
		return nil, 0, domain.ErrConflict
	}
	_, err := tx.Exec(ctx, "INSERT INTO personal_models(workspace_id,id,label,provider,model,input_price,output_price,credential) VALUES($1,$2,$3,$4,$5,$6,$7,$8)", w, m.ID, m.Label, m.Provider, m.Model, m.InputPrice, m.OutputPrice, m.Credential)
	if err == nil {
		err = event(ctx, tx, w, "model.saved", m.ID)
	}
	return m, 201, err
}
func (s *Store) DefaultModel(ctx context.Context, tx pgx.Tx, w, id string) (any, int, error) {
	if id != "" {
		if _, err := s.SelectedModel(ctx, tx, w, id); err != nil {
			return nil, 0, err
		}
	}
	_, err := tx.Exec(ctx, "INSERT INTO workspace_model_settings(workspace_id,model_id) VALUES($1,$2) ON CONFLICT(workspace_id) DO UPDATE SET model_id=EXCLUDED.model_id", w, id)
	if err == nil {
		err = event(ctx, tx, w, "model.selected", id)
	}
	return map[string]string{"default_id": id}, 200, err
}

// Empty requested ID selects the workspace default. Immutable configurations
// are share-locked until task creation has captured their identity.
func (s *Store) SelectedModel(ctx context.Context, tx pgx.Tx, w, id string) (*PersonalModel, error) {
	if id == "" {
		err := tx.QueryRow(ctx, "SELECT model_id FROM workspace_model_settings WHERE workspace_id=$1", w).Scan(&id)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		if id == "" {
			return nil, nil
		}
	}
	m, err := readPersonalModel(tx.QueryRow(ctx, "SELECT "+modelColumns+" FROM personal_models WHERE workspace_id=$1 AND id=$2 AND NOT revoked FOR SHARE", w, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrConflict
	}
	return &m, err
}
func (s *Store) RemoveModel(ctx context.Context, tx pgx.Tx, w, id string) (any, int, error) {
	tag, err := tx.Exec(ctx, "UPDATE personal_models SET revoked=true,credential=''::bytea WHERE workspace_id=$1 AND id=$2 AND NOT revoked", w, id)
	if err != nil {
		return nil, 0, err
	}
	if tag.RowsAffected() == 0 {
		return nil, 0, domain.ErrNotFound
	}
	_, err = tx.Exec(ctx, "UPDATE workspace_model_settings SET model_id='' WHERE workspace_id=$1 AND model_id=$2", w, id)
	if err == nil {
		err = event(ctx, tx, w, "model.removed", id)
	}
	return map[string]bool{"ok": true}, 200, err
}
func (s *Store) RunModel(ctx context.Context, r Ref) (*PersonalModel, error) {
	var id string
	if err := s.Pool.QueryRow(ctx, "SELECT model_config_id FROM runs WHERE workspace_id=$1 AND id=$2", r.Workspace, r.ID).Scan(&id); err != nil {
		return nil, err
	}
	if id == "" {
		return nil, nil
	}
	m, err := readPersonalModel(s.Pool.QueryRow(ctx, "SELECT "+modelColumns+" FROM personal_models WHERE workspace_id=$1 AND id=$2 AND NOT revoked", r.Workspace, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errors.New("MODEL_CONFIG_REVOKED")
	}
	return &m, err
}
func (s *Store) BindRunModel(ctx context.Context, tx pgx.Tx, w, run, id string) error {
	_, err := tx.Exec(ctx, "UPDATE runs SET model_config_id=$3 WHERE workspace_id=$1 AND id=$2", w, run, id)
	return err
}

type AppConnection struct {
	Label      string
	Revision   int
	Credential []byte
	Enabled    bool
	VerifiedAt *time.Time
}

func (s *Store) Connection(ctx context.Context, w, id string) (AppConnection, error) {
	var b AppConnection
	err := s.Pool.QueryRow(ctx, "SELECT label,revision,credential,enabled,verified_at FROM app_connections WHERE workspace_id=$1 AND id=$2", w, id).Scan(&b.Label, &b.Revision, &b.Credential, &b.Enabled, &b.VerifiedAt)
	return b, err
}
func (s *Store) SaveConnection(ctx context.Context, tx pgx.Tx, w, id, label string, expected int, encrypted []byte, enabled bool) (any, int, error) {
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,5))", w+":"+id); err != nil {
		return nil, 0, err
	}
	var current int
	err := tx.QueryRow(ctx, "SELECT revision FROM app_connections WHERE workspace_id=$1 AND id=$2", w, id).Scan(&current)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, err
	}
	if current != expected {
		return nil, 0, domain.ErrConflict
	}
	_, err = tx.Exec(ctx, "INSERT INTO app_connections(workspace_id,id,label,revision,credential,enabled) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(workspace_id,id) DO UPDATE SET label=EXCLUDED.label,revision=EXCLUDED.revision,credential=EXCLUDED.credential,enabled=EXCLUDED.enabled,verified_at=NULL", w, id, label, current+1, encrypted, enabled)
	if err == nil {
		err = event(ctx, tx, w, "connection.updated", id)
	}
	return map[string]any{"ok": true, "revision": current + 1}, 200, err
}
func (s *Store) VerifyConnection(ctx context.Context, w, id string, revision int) error {
	_, err := s.Pool.Exec(ctx, "UPDATE app_connections SET verified_at=now() WHERE workspace_id=$1 AND id=$2 AND revision=$3 AND enabled", w, id, revision)
	return err
}
