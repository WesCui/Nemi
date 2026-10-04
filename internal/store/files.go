package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"nemi/internal/domain"
)

type FileRecord struct {
	domain.File
	Storage, ObjectKey string
}

const fileColumns = "f.id,f.name,f.kind,f.mime,f.size,f.origin_url,f.created_at,f.storage,f.object_key"

func scanFile(row pgx.Row) (FileRecord, error) {
	var f FileRecord
	err := row.Scan(&f.ID, &f.Name, &f.Kind, &f.MIME, &f.Size, &f.OriginURL, &f.CreatedAt, &f.Storage, &f.ObjectKey)
	if errors.Is(err, pgx.ErrNoRows) {
		err = domain.ErrNotFound
	}
	return f, err
}
func (s *Store) File(ctx context.Context, w, id string) (FileRecord, error) {
	return scanFile(s.Pool.QueryRow(ctx, "SELECT "+fileColumns+" FROM workspace_files f WHERE f.workspace_id=$1 AND f.id=$2 AND NOT f.deleted", w, id))
}

// A file is visible to the agent only after explicit attachment to this chat,
// or when this chat produced it. Later turns and other chats cannot leak in.
const accessibleFile = ` EXISTS(SELECT 1 FROM chat_run_files b JOIN chat_turns t ON t.workspace_id=b.workspace_id AND t.run_id=b.run_id JOIN chat_turns current ON current.workspace_id=t.workspace_id AND current.conversation_id=t.conversation_id WHERE current.workspace_id=$1 AND current.run_id=$2 AND b.file_id=f.id AND t.position<=current.position)`

func (s *Store) AgentFile(ctx context.Context, r Ref, id string) (FileRecord, error) {
	return scanFile(s.Pool.QueryRow(ctx, "SELECT "+fileColumns+" FROM workspace_files f WHERE f.workspace_id=$1 AND NOT f.deleted AND f.id=$3 AND"+accessibleFile, r.Workspace, r.ID, id))
}
func (s *Store) AgentFiles(ctx context.Context, r Ref) ([]domain.File, error) {
	rows, err := s.Pool.Query(ctx, "SELECT "+fileColumns+" FROM workspace_files f WHERE f.workspace_id=$1 AND NOT f.deleted AND"+accessibleFile+" ORDER BY f.created_at,f.id", r.Workspace, r.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.File{}
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f.File)
	}
	return out, rows.Err()
}

// Quota and run fences are acquired before writing an object. The transaction
// commits metadata and conversation binding together; objects are immutable.
func (s *Store) AdmitFile(ctx context.Context, tx pgx.Tx, w string, r *Ref, size int64) error {
	if r != nil {
		if _, err := activeChat(ctx, tx, *r); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", w+":file-quota"); err != nil {
		return err
	}
	var count int
	var used int64
	if err := tx.QueryRow(ctx, "SELECT count(*),COALESCE(sum(size),0) FROM workspace_files WHERE workspace_id=$1", w).Scan(&count, &used); err != nil {
		return err
	}
	if count >= 200 || used+size > 100<<20 {
		return errors.New("FILE_QUOTA_EXCEEDED")
	}
	return nil
}
func (s *Store) InsertFile(ctx context.Context, tx pgx.Tx, w string, f FileRecord, r *Ref) error {
	_, err := tx.Exec(ctx, `INSERT INTO workspace_files(workspace_id,id,name,kind,mime,size,origin_url,created_at,storage,object_key) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, w, f.ID, f.Name, f.Kind, f.MIME, f.Size, f.OriginURL, f.CreatedAt, f.Storage, f.ObjectKey)
	if err != nil {
		return err
	}
	if r != nil {
		_, err = tx.Exec(ctx, "INSERT INTO chat_run_files(workspace_id,run_id,file_id,role) VALUES($1,$2,$3,'output')", w, r.ID, f.ID)
	}
	if err != nil {
		return err
	}
	return event(ctx, tx, w, "file.created", f.ID)
}

func bindChatFiles(ctx context.Context, tx pgx.Tx, w, run string, ids []string) error {
	if len(ids) > 4 {
		return errors.New("FILE_ATTACHMENT_INVALID")
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if len(id) != 32 || seen[id] {
			return errors.New("FILE_ATTACHMENT_INVALID")
		}
		seen[id] = true
		var exists bool
		if err := tx.QueryRow(ctx, "SELECT NOT deleted FROM workspace_files WHERE workspace_id=$1 AND id=$2 FOR SHARE", w, id).Scan(&exists); err != nil || !exists {
			return errors.New("FILE_ATTACHMENT_INVALID")
		}
		if _, err := tx.Exec(ctx, "INSERT INTO chat_run_files(workspace_id,run_id,file_id,role) VALUES($1,$2,$3,'input')", w, run, id); err != nil {
			return err
		}
	}
	return nil
}
