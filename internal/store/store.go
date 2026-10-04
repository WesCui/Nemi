package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"nemi/internal/domain"
)

//go:embed schema.sql
var schema string

type Store struct{ Pool *pgxpool.Pool }
type Identity struct {
	UserID    string `json:"id"`
	Workspace string `json:"-"`
	Name      string `json:"name"`
}

func Open(ctx context.Context, url string) (*Store, error) {
	p, e := pgxpool.New(ctx, url)
	if e != nil {
		return nil, e
	}
	if e = p.Ping(ctx); e != nil {
		p.Close()
		return nil, e
	}
	return &Store{p}, nil
}
func (s *Store) Migrate(ctx context.Context) error {
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(73310001)"); e != nil {
		return e
	}
	// Completed schema versions are transactional. Avoid reacquiring broad DDL
	// locks on every API/worker start while another worker settles live runs.
	var exists bool
	if e = tx.QueryRow(ctx, "SELECT to_regclass('schema_versions') IS NOT NULL").Scan(&exists); e != nil {
		return e
	}
	if exists {
		var version int
		if e = tx.QueryRow(ctx, "SELECT COALESCE(max(version),0) FROM schema_versions").Scan(&version); e != nil {
			return e
		}
		if version > 8 {
			return errors.New("database schema is newer than this runtime")
		}
		if version == 8 {
			return tx.Commit(ctx)
		}
	}
	if _, e = tx.Exec(ctx, schema); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *Store) Bootstrap(ctx context.Context, code string, ownerIDs ...string) error {
	owner := "local-owner"
	if len(ownerIDs) > 0 {
		owner = ownerIDs[0]
	}
	if len(owner) < 1 || len(owner) > 100 {
		return errors.New("invalid bootstrap identity")
	}
	h := sha256.Sum256([]byte(code))
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "INSERT INTO workspaces(id) VALUES($1) ON CONFLICT DO NOTHING", owner); e != nil {
		return e
	}
	_, e = tx.Exec(ctx, "INSERT INTO users(id,workspace_id,invite_hash,display_name) VALUES($1,$1,$2,'朋友') ON CONFLICT(id) DO UPDATE SET invite_hash=EXCLUDED.invite_hash", owner, h[:])
	if e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *Store) Login(ctx context.Context, code string) (string, error) {
	h := sha256.Sum256([]byte(code))
	var u string
	if e := s.Pool.QueryRow(ctx, "SELECT id FROM users WHERE invite_hash=$1", h[:]).Scan(&u); e != nil {
		return "", domain.ErrNotFound
	}
	token := domain.ID() + domain.ID()
	th := sha256.Sum256([]byte(token))
	_, e := s.Pool.Exec(ctx, "INSERT INTO sessions(token_hash,user_id,expires_at) VALUES($1,$2,$3)", th[:], u, time.Now().Add(7*24*time.Hour))
	return token, e
}
func (s *Store) Authenticate(ctx context.Context, token string) (Identity, error) {
	h := sha256.Sum256([]byte(token))
	var i Identity
	e := s.Pool.QueryRow(ctx, "SELECT u.id,u.workspace_id,u.display_name FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=$1 AND s.expires_at>now()", h[:]).Scan(&i.UserID, &i.Workspace, &i.Name)
	return i, e
}
func (s *Store) Logout(ctx context.Context, token string) error {
	h := sha256.Sum256([]byte(token))
	_, e := s.Pool.Exec(ctx, "DELETE FROM sessions WHERE token_hash=$1", h[:])
	return e
}
func event(ctx context.Context, tx pgx.Tx, w, kind, id string) error {
	// Serialize allocation until commit so SSE never skips a lower sequence
	// that commits after a later concurrent transaction.
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(73310003)"); err != nil {
		return err
	}
	_, e := tx.Exec(ctx, "INSERT INTO business_events(workspace_id,kind,subject_id) VALUES($1,$2,$3)", w, kind, id)
	return e
}
func enqueue(ctx context.Context, tx pgx.Tx, w, kind, id string, rev int, due *time.Time) error {
	_, e := tx.Exec(ctx, "INSERT INTO outbox(id,workspace_id,kind,subject_id,revision,due_at) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING", domain.ID(), w, kind, id, rev, due)
	return e
}

type CommandResult struct {
	Body   json.RawMessage
	Status int
}

// A retry with the same key is a read of the original committed result.
func (s *Store) Command(ctx context.Context, w, key, route string, body []byte, fn func(pgx.Tx) (any, int, error)) (CommandResult, error) {
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return CommandResult{}, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", w+":"+key); e != nil {
		return CommandResult{}, e
	}
	h := sha256.Sum256(body)
	var oldHash, oldBody []byte
	var oldRoute string
	var code int
	e = tx.QueryRow(ctx, "SELECT route,body_hash,response,status FROM commands WHERE workspace_id=$1 AND key=$2", w, key).Scan(&oldRoute, &oldHash, &oldBody, &code)
	if e == nil {
		if oldRoute != route || !bytes.Equal(oldHash, h[:]) {
			return CommandResult{}, domain.ErrConflict
		}
		return CommandResult{oldBody, code}, nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return CommandResult{}, e
	}
	v, code, e := fn(tx)
	if e != nil {
		return CommandResult{}, e
	}
	out, e := json.Marshal(v)
	if e != nil {
		return CommandResult{}, e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO commands(workspace_id,key,route,body_hash,response,status) VALUES($1,$2,$3,$4,$5,$6)", w, key, route, h[:], out, code); e != nil {
		return CommandResult{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return CommandResult{}, e
	}
	return CommandResult{out, code}, nil
}

const matterSelect = "SELECT id,title,source,category,status,revision,items,deadline,created_at,origin_url,origin_provider FROM matters"

func scanMatter(row pgx.Row) (domain.Matter, error) {
	var m domain.Matter
	var items []byte
	e := row.Scan(&m.ID, &m.Title, &m.Source, &m.Category, &m.Status, &m.Revision, &items, &m.Deadline, &m.CreatedAt, &m.OriginURL, &m.OriginProvider)
	if errors.Is(e, pgx.ErrNoRows) {
		return m, domain.ErrNotFound
	}
	if e != nil {
		return m, e
	}
	e = json.Unmarshal(items, &m.Items)
	return m, e
}
func (s *Store) CreateMatter(ctx context.Context, tx pgx.Tx, w string, c domain.CreateMatter) (any, int, error) {
	m := domain.Matter{ID: domain.ID(), Title: c.Title, Source: c.Source, Category: c.Category, Status: "ACTIVE", Revision: 1, Items: []domain.Item{}, Deadline: c.Deadline, CreatedAt: time.Now().UTC()}
	m.OriginURL, m.OriginProvider = c.OriginURL, c.OriginProvider
	_, e := tx.Exec(ctx, "INSERT INTO matters(workspace_id,id,title,source,category,status,deadline,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)", w, m.ID, m.Title, m.Source, m.Category, m.Status, m.Deadline, m.CreatedAt)
	if e != nil {
		return nil, 0, e
	}
	if c.OriginURL != "" {
		if _, e = tx.Exec(ctx, "UPDATE matters SET origin_url=$3,origin_provider=$4 WHERE workspace_id=$1 AND id=$2", w, m.ID, c.OriginURL, c.OriginProvider); e != nil {
			return nil, 0, e
		}
	}
	if c.ReminderAt != nil {
		id := domain.ID()
		due := domain.EffectiveDue(*c.ReminderAt, c.Quiet)
		_, e = tx.Exec(ctx, "INSERT INTO reminders(workspace_id,id,matter_id,nominal_at,due_at,quiet,repeat,repeat_until) VALUES($1,$2,$3,$4,$5,$6,$7,$8)", w, id, m.ID, c.ReminderAt, due, c.Quiet, domain.NormalizeRepeat(c.Repeat), c.Until)
		if e != nil {
			return nil, 0, e
		}
		if e = enqueue(ctx, tx, w, "reminder", id, 1, &due); e != nil {
			return nil, 0, e
		}
	}
	if e = event(ctx, tx, w, "matter.created", m.ID); e != nil {
		return nil, 0, e
	}
	return m, 201, nil
}

type EditMatter struct {
	Expected int            `json:"expected_revision"`
	Title    *string        `json:"title"`
	Source   *string        `json:"source"`
	Status   *string        `json:"status"`
	Items    *[]domain.Item `json:"items"`
}

func (s *Store) EditMatter(ctx context.Context, tx pgx.Tx, w, id string, p EditMatter) (any, int, error) {
	m, e := scanMatter(tx.QueryRow(ctx, matterSelect+" WHERE workspace_id=$1 AND id=$2 FOR UPDATE", w, id))
	if e != nil {
		return nil, 0, e
	}
	if m.Revision != p.Expected {
		return nil, 0, domain.ErrConflict
	}
	if p.Title != nil {
		m.Title = *p.Title
	}
	if p.Source != nil {
		m.Source = *p.Source
	}
	if p.Items != nil {
		m.Items = *p.Items
	}
	if p.Status != nil {
		m.Status = *p.Status
	}
	m.Revision++
	items, _ := json.Marshal(m.Items)
	_, e = tx.Exec(ctx, "UPDATE matters SET title=$3,status=$4,items=$5,revision=$6,source=$7 WHERE workspace_id=$1 AND id=$2", w, id, m.Title, m.Status, items, m.Revision, m.Source)
	if e != nil {
		return nil, 0, e
	}
	if m.Status == "COMPLETED" {
		var rid string
		var rev int
		e = tx.QueryRow(ctx, "UPDATE reminders SET enabled=false,revision=revision+1,sync_status='DISABLED' WHERE workspace_id=$1 AND matter_id=$2 AND enabled RETURNING id,revision-1", w, id).Scan(&rid, &rev)
		if e == nil {
			if e = enqueue(ctx, tx, w, "cancel_reminder", rid, rev, nil); e != nil {
				return nil, 0, e
			}
		} else if !errors.Is(e, pgx.ErrNoRows) {
			return nil, 0, e
		}
	}
	if e = event(ctx, tx, w, "matter.updated", id); e != nil {
		return nil, 0, e
	}
	return m, 200, nil
}

type SaveReminder struct {
	Expected  int        `json:"expected_revision"`
	At        *time.Time `json:"at"`
	Enabled   bool       `json:"enabled"`
	Quiet     bool       `json:"quiet"`
	Confirmed bool       `json:"confirmed"`
	Timezone  string     `json:"timezone"`
	Repeat    string     `json:"repeat"`
	Until     *time.Time `json:"repeat_until"`
}

func (s *Store) SaveReminder(ctx context.Context, tx pgx.Tx, w, mid string, p SaveReminder) (any, int, error) {
	m, e := scanMatter(tx.QueryRow(ctx, matterSelect+" WHERE workspace_id=$1 AND id=$2 FOR UPDATE", w, mid))
	if e != nil {
		return nil, 0, e
	}
	if m.Status != "ACTIVE" {
		return nil, 0, domain.ErrConflict
	}
	var id string
	var rev int
	e = tx.QueryRow(ctx, "SELECT id,revision FROM reminders WHERE workspace_id=$1 AND matter_id=$2 FOR UPDATE", w, mid).Scan(&id, &rev)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return nil, 0, e
	}
	if rev != p.Expected {
		return nil, 0, domain.ErrConflict
	}
	if rev > 0 {
		if e = enqueue(ctx, tx, w, "cancel_reminder", id, rev, nil); e != nil {
			return nil, 0, e
		}
	} else {
		id = domain.ID()
	}
	if p.At == nil {
		return nil, 0, fmt.Errorf("reminder date required")
	}
	due := domain.EffectiveDue(*p.At, p.Quiet)
	status := "DISABLED"
	if p.Enabled {
		status = "PENDING_SYNC"
	}
	if p.Enabled {
		// API validates the time window; the store also enforces the matter deadline.
		if m.Deadline != nil && (due.After(*m.Deadline) || (p.Until != nil && p.Until.After(*m.Deadline))) {
			return nil, 0, domain.ErrConflict
		}
	}
	_, e = tx.Exec(ctx, `INSERT INTO reminders(workspace_id,id,matter_id,revision,nominal_at,due_at,quiet,enabled,sync_status,repeat,repeat_until) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
 ON CONFLICT(workspace_id,matter_id) DO UPDATE SET revision=EXCLUDED.revision,nominal_at=EXCLUDED.nominal_at,due_at=EXCLUDED.due_at,quiet=EXCLUDED.quiet,enabled=EXCLUDED.enabled,sync_status=EXCLUDED.sync_status,repeat=EXCLUDED.repeat,repeat_until=EXCLUDED.repeat_until`, w, id, mid, rev+1, p.At, due, p.Quiet, p.Enabled, status, domain.NormalizeRepeat(p.Repeat), p.Until)
	if e != nil {
		return nil, 0, e
	}
	if p.Enabled {
		if e = enqueue(ctx, tx, w, "reminder", id, rev+1, &due); e != nil {
			return nil, 0, e
		}
	}
	if e = event(ctx, tx, w, "reminder.updated", id); e != nil {
		return nil, 0, e
	}
	return domain.Reminder{ID: id, MatterID: mid, Title: m.Title, Revision: rev + 1, Nominal: *p.At, Due: due, Quiet: p.Quiet, Enabled: p.Enabled, SyncStatus: status, Repeat: domain.NormalizeRepeat(p.Repeat), Until: p.Until}, 200, nil
}
func (s *Store) CreateRun(ctx context.Context, tx pgx.Tx, w, mid, mode, profile string, expected int, useMemory ...bool) (any, int, error) {
	m, e := scanMatter(tx.QueryRow(ctx, matterSelect+" WHERE workspace_id=$1 AND id=$2 FOR UPDATE", w, mid))
	if e != nil {
		return nil, 0, e
	}
	if m.Revision != expected || m.Status != "ACTIVE" {
		return nil, 0, domain.ErrConflict
	}
	var existing string
	e = tx.QueryRow(ctx, "SELECT id FROM runs WHERE workspace_id=$1 AND matter_id=$2 AND status IN ('QUEUED','RUNNING')", w, mid).Scan(&existing)
	if e == nil {
		return nil, 0, domain.ErrBusy
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return nil, 0, e
	}
	r := domain.Run{ID: domain.ID(), MatterID: mid, Status: "QUEUED", Mode: mode, CreatedAt: time.Now().UTC()}
	refs := []MemoryRef{}
	if len(useMemory) > 0 && useMemory[0] {
		refs, e = selectMemoryRefs(ctx, tx, w, m.Category)
		if e != nil {
			return nil, 0, e
		}
	}
	refJSON, _ := json.Marshal(refs)
	_, e = tx.Exec(ctx, "INSERT INTO runs(workspace_id,id,matter_id,matter_revision,snapshot_title,snapshot_source,status,mode,model_profile,created_at,memory_refs) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)", w, r.ID, mid, m.Revision, m.Title, m.Source, r.Status, mode, profile, r.CreatedAt, refJSON)
	if e != nil {
		return nil, 0, e
	}
	if e = enqueue(ctx, tx, w, "run", r.ID, 0, nil); e != nil {
		return nil, 0, e
	}
	if e = event(ctx, tx, w, "run.queued", r.ID); e != nil {
		return nil, 0, e
	}
	return r, 202, nil
}

type Dashboard struct {
	User          Identity              `json:"user"`
	Matters       []domain.Matter       `json:"matters"`
	Reminders     []domain.Reminder     `json:"reminders"`
	Runs          []domain.Run          `json:"runs"`
	Notifications []domain.Notification `json:"notifications"`
	ModelMode     string                `json:"model_mode"`
	Memories      []domain.Memory       `json:"memories"`
	Activity      []domain.Event        `json:"activity"`
}

func (s *Store) Dashboard(ctx context.Context, i Identity, mode string) (Dashboard, error) {
	d := Dashboard{User: i, Matters: []domain.Matter{}, Reminders: []domain.Reminder{}, Runs: []domain.Run{}, Notifications: []domain.Notification{}, ModelMode: mode, Memories: []domain.Memory{}, Activity: []domain.Event{}}
	// One snapshot prevents a task and its revision-dependent reminders being shown inconsistently.
	tx, e := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return d, e
	}
	defer tx.Rollback(ctx)
	rows, e := tx.Query(ctx, matterSelect+" WHERE workspace_id=$1 ORDER BY created_at DESC LIMIT 200", i.Workspace)
	if e != nil {
		return d, e
	}
	for rows.Next() {
		m, err := scanMatter(rows)
		if err != nil {
			rows.Close()
			return d, err
		}
		d.Matters = append(d.Matters, m)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return d, e
	}
	rows, e = tx.Query(ctx, "SELECT r.id,r.matter_id,m.title,r.revision,r.nominal_at,r.due_at,r.quiet,r.enabled,r.sync_status,r.repeat,r.repeat_until FROM reminders r JOIN matters m ON (r.workspace_id=m.workspace_id AND r.matter_id=m.id) WHERE r.workspace_id=$1 ORDER BY r.due_at LIMIT 200", i.Workspace)
	if e != nil {
		return d, e
	}
	for rows.Next() {
		var r domain.Reminder
		if e = rows.Scan(&r.ID, &r.MatterID, &r.Title, &r.Revision, &r.Nominal, &r.Due, &r.Quiet, &r.Enabled, &r.SyncStatus, &r.Repeat, &r.Until); e != nil {
			rows.Close()
			return d, e
		}
		d.Reminders = append(d.Reminders, r)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return d, e
	}
	rows, e = tx.Query(ctx, "SELECT id,matter_id,status,mode,result,error_code,created_at,used_memory_count FROM runs WHERE workspace_id=$1 AND kind='plan' ORDER BY created_at DESC LIMIT 200", i.Workspace)
	if e != nil {
		return d, e
	}
	for rows.Next() {
		var r domain.Run
		var b []byte
		if e = rows.Scan(&r.ID, &r.MatterID, &r.Status, &r.Mode, &b, &r.Error, &r.CreatedAt, &r.UsedMemories); e != nil {
			rows.Close()
			return d, e
		}
		if b != nil {
			if e = json.Unmarshal(b, &r.Result); e != nil {
				rows.Close()
				return d, e
			}
		}
		d.Runs = append(d.Runs, r)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return d, e
	}
	rows, e = tx.Query(ctx, "SELECT id,title,matter_id,status,created_at FROM notifications WHERE workspace_id=$1 ORDER BY created_at DESC LIMIT 100", i.Workspace)
	if e != nil {
		return d, e
	}
	for rows.Next() {
		var n domain.Notification
		if e = rows.Scan(&n.ID, &n.Title, &n.MatterID, &n.Status, &n.CreatedAt); e != nil {
			rows.Close()
			return d, e
		}
		d.Notifications = append(d.Notifications, n)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return d, e
	}
	rows, e = tx.Query(ctx, "SELECT id,category,content,revision,updated_at FROM memories WHERE workspace_id=$1 ORDER BY updated_at DESC,id LIMIT 50", i.Workspace)
	if e != nil {
		return d, e
	}
	for rows.Next() {
		var m domain.Memory
		if e = rows.Scan(&m.ID, &m.Category, &m.Text, &m.Revision, &m.UpdatedAt); e != nil {
			rows.Close()
			return d, e
		}
		d.Memories = append(d.Memories, m)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return d, e
	}
	rows, e = tx.Query(ctx, "SELECT sequence,kind,subject_id,created_at FROM business_events WHERE workspace_id=$1 ORDER BY sequence DESC LIMIT 80", i.Workspace)
	if e != nil {
		return d, e
	}
	for rows.Next() {
		var ev domain.Event
		if e = rows.Scan(&ev.Sequence, &ev.Kind, &ev.SubjectID, &ev.CreatedAt); e != nil {
			rows.Close()
			return d, e
		}
		d.Activity = append(d.Activity, ev)
	}
	e = rows.Err()
	rows.Close()
	return d, e
}
func (s *Store) Events(ctx context.Context, w string, after int64) ([]domain.Event, error) {
	rows, e := s.Pool.Query(ctx, "SELECT sequence,kind,subject_id,created_at FROM business_events WHERE workspace_id=$1 AND sequence>$2 ORDER BY sequence LIMIT 100", w, after)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	es := []domain.Event{}
	for rows.Next() {
		var ev domain.Event
		if e = rows.Scan(&ev.Sequence, &ev.Kind, &ev.SubjectID, &ev.CreatedAt); e != nil {
			return nil, e
		}
		es = append(es, ev)
	}
	return es, rows.Err()
}
