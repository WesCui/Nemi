package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"nemi/internal/domain"
)

type Ref struct {
	Workspace string
	ID        string
}
type RunInput struct {
	Title, Source, Profile string
	Reservation            int64
	Ready, Done            bool
}

// Reserve is serialized with admissions so concurrent workers cannot overspend.
func (s *Store) AdmitRun(ctx context.Context, r Ref, reserve func(string, string) int64) (RunInput, error) {
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return RunInput{}, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(73310002)"); e != nil {
		return RunInput{}, e
	}
	var in RunInput
	var status, attempt string
	e = tx.QueryRow(ctx, "SELECT snapshot_title,snapshot_source,model_profile,status,attempt_status,reserved_micro_cny FROM runs WHERE workspace_id=$1 AND id=$2 FOR UPDATE", r.Workspace, r.ID).Scan(&in.Title, &in.Source, &in.Profile, &status, &attempt, &in.Reservation)
	if e != nil {
		return in, e
	}
	if status == "SUCCEEDED" || status == "FAILED" {
		in.Done = true
		return in, nil
	}
	if status == "RUNNING" && attempt == "RESERVED" {
		in.Ready = true
		return in, nil
	}
	if attempt != "NONE" {
		return in, errors.New("MODEL_OUTCOME_UNKNOWN")
	}
	var global, personal int
	e = tx.QueryRow(ctx, "SELECT count(*),count(*) FILTER(WHERE workspace_id=$1) FROM runs WHERE status='RUNNING'", r.Workspace).Scan(&global, &personal)
	if e != nil {
		return in, e
	}
	if global >= 5 || personal >= 2 {
		return in, nil
	}
	in.Reservation = reserve(in.Title, in.Source)
	if in.Reservation > 1000000 {
		return in, errors.New("RUN_BUDGET_EXCEEDED")
	}
	var daily int64
	e = tx.QueryRow(ctx, "SELECT COALESCE(sum(GREATEST(reserved_micro_cny,charged_micro_cny)),0) FROM runs WHERE workspace_id=$1 AND budget_day=(now() AT TIME ZONE 'Asia/Shanghai')::date", r.Workspace).Scan(&daily)
	if e != nil {
		return in, e
	}
	if daily+in.Reservation > 3000000 {
		return in, errors.New("DAILY_BUDGET_EXCEEDED")
	}
	_, e = tx.Exec(ctx, "UPDATE runs SET status='RUNNING',attempt_status='RESERVED',reserved_micro_cny=$3,budget_day=(now() AT TIME ZONE 'Asia/Shanghai')::date,updated_at=now() WHERE workspace_id=$1 AND id=$2", r.Workspace, r.ID, in.Reservation)
	if e != nil {
		return in, e
	}
	if e = event(ctx, tx, r.Workspace, "run.running", r.ID); e != nil {
		return in, e
	}
	if e = tx.Commit(ctx); e != nil {
		return in, e
	}
	in.Ready = true
	return in, nil
}
func (s *Store) ClaimModel(ctx context.Context, r Ref) (RunInput, error) {
	var in RunInput
	e := s.Pool.QueryRow(ctx, "UPDATE runs SET attempt_status='CALLING',updated_at=now() WHERE workspace_id=$1 AND id=$2 AND status='RUNNING' AND attempt_status='RESERVED' RETURNING snapshot_title,snapshot_source,model_profile,reserved_micro_cny", r.Workspace, r.ID).Scan(&in.Title, &in.Source, &in.Profile, &in.Reservation)
	if errors.Is(e, pgx.ErrNoRows) {
		return in, errors.New("MODEL_OUTCOME_UNKNOWN")
	}
	return in, e
}
func (s *Store) FinishRun(ctx context.Context, r Ref, plan domain.Plan, input, output, cost int64) error {
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var status, mid string
	var rev int
	e = tx.QueryRow(ctx, "SELECT status,matter_id,matter_revision FROM runs WHERE workspace_id=$1 AND id=$2 FOR UPDATE", r.Workspace, r.ID).Scan(&status, &mid, &rev)
	if e != nil {
		return e
	}
	if status == "SUCCEEDED" {
		return nil
	}
	if status != "RUNNING" {
		return errors.New("RUN_NO_LONGER_ACTIVE")
	}
	b, _ := json.Marshal(plan)
	_, e = tx.Exec(ctx, "UPDATE runs SET status='SUCCEEDED',result=$3,attempt_status='SETTLED',charged_micro_cny=$4,reserved_micro_cny=0,input_tokens=$5,output_tokens=$6,updated_at=now() WHERE workspace_id=$1 AND id=$2", r.Workspace, r.ID, b, cost, input, output)
	if e != nil {
		return e
	}
	items := []domain.Item{}
	for _, v := range plan.Items {
		items = append(items, domain.Item{Text: v})
	}
	ib, _ := json.Marshal(items)
	// Results remain available on the Run, but never overwrite a user's newer checklist.
	_, e = tx.Exec(ctx, "UPDATE matters SET items=$3,revision=revision+1 WHERE workspace_id=$1 AND id=$2 AND revision=$4 AND status='ACTIVE'", r.Workspace, mid, ib, rev)
	if e != nil {
		return e
	}
	if e = event(ctx, tx, r.Workspace, "run.succeeded", r.ID); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *Store) FailRun(ctx context.Context, r Ref, code string, input, output, cost int64) error {
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	// A completed activity result may have committed just before the workflow timed out.
	tag, e := tx.Exec(ctx, `UPDATE runs SET status='FAILED',error_code=$3,
 attempt_status=CASE WHEN attempt_status='CALLING' AND $4=0 THEN 'UNKNOWN' ELSE 'SETTLED' END,
 reserved_micro_cny=CASE WHEN attempt_status='CALLING' AND $4=0 THEN reserved_micro_cny ELSE 0 END,
 charged_micro_cny=$4,input_tokens=$5,output_tokens=$6,updated_at=now()
 WHERE workspace_id=$1 AND id=$2 AND status IN ('QUEUED','RUNNING')`, r.Workspace, r.ID, code, cost, input, output)
	if e != nil {
		return e
	}
	if tag.RowsAffected() > 0 {
		if e = event(ctx, tx, r.Workspace, "run.failed", r.ID); e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}

// Local inbox insertion is the side effect. Revision check and insertion are atomic.
func (s *Store) DeliverReminder(ctx context.Context, r Ref, revision int) error {
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var enabled bool
	var rev int
	var due time.Time
	var mid, title, status string
	e = tx.QueryRow(ctx, "SELECT r.enabled,r.revision,r.due_at,r.matter_id,m.title,m.status FROM reminders r JOIN matters m ON(r.workspace_id=m.workspace_id AND r.matter_id=m.id) WHERE r.workspace_id=$1 AND r.id=$2 FOR UPDATE OF r,m", r.Workspace, r.ID).Scan(&enabled, &rev, &due, &mid, &title, &status)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil
	}
	if e != nil {
		return e
	}
	if !enabled || rev != revision || status != "ACTIVE" {
		return nil
	}
	if time.Now().Before(due) {
		return errors.New("REMINDER_NOT_DUE")
	}
	deliveryStatus := "AVAILABLE"
	if time.Since(due) > 10*time.Minute {
		deliveryStatus = "OVERDUE"
	}
	tag, e := tx.Exec(ctx, "INSERT INTO notifications(workspace_id,id,reminder_id,revision,matter_id,title,status) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(workspace_id,reminder_id,revision) DO NOTHING", r.Workspace, domain.ID(), r.ID, revision, mid, title, deliveryStatus)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, "UPDATE reminders SET sync_status='FIRED' WHERE workspace_id=$1 AND id=$2", r.Workspace, r.ID)
	if e != nil {
		return e
	}
	if tag.RowsAffected() > 0 {
		if e = event(ctx, tx, r.Workspace, "notification.available", r.ID); e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}

type Outbox struct {
	ID, Workspace, Kind, Subject, Lease string
	Revision                            int
	Due                                 *time.Time
}

func (s *Store) LeaseOutbox(ctx context.Context) (*Outbox, error) {
	o := Outbox{Lease: domain.ID()}
	e := s.Pool.QueryRow(ctx, `UPDATE outbox SET leased_until=now()+interval '30 seconds',lease_token=$1,attempts=attempts+1 WHERE id=(
 SELECT id FROM outbox WHERE state='PENDING' AND available_at<=now() AND (leased_until IS NULL OR leased_until<now()) ORDER BY available_at,id FOR UPDATE SKIP LOCKED LIMIT 1
 ) RETURNING id,workspace_id,kind,subject_id,revision,due_at`, o.Lease).Scan(&o.ID, &o.Workspace, &o.Kind, &o.Subject, &o.Revision, &o.Due)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, nil
	}
	return &o, e
}
func (s *Store) CompleteOutbox(ctx context.Context, o Outbox) error {
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	tag, e := tx.Exec(ctx, "UPDATE outbox SET state='DONE',leased_until=NULL,last_error='' WHERE id=$1 AND lease_token=$2", o.ID, o.Lease)
	if e != nil {
		return e
	}
	if o.Kind == "reminder" && tag.RowsAffected() > 0 {
		_, e = tx.Exec(ctx, "UPDATE reminders SET sync_status='APPLIED' WHERE workspace_id=$1 AND id=$2 AND revision=$3 AND sync_status='PENDING_SYNC'", o.Workspace, o.Subject, o.Revision)
		if e != nil {
			return e
		}
		if e = event(ctx, tx, o.Workspace, "reminder.synced", o.Subject); e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}
func (s *Store) RetryOutbox(ctx context.Context, o Outbox) error {
	_, e := s.Pool.Exec(ctx, "UPDATE outbox SET leased_until=NULL,available_at=now()+interval '5 seconds',last_error='ENGINE_UNAVAILABLE' WHERE id=$1 AND lease_token=$2", o.ID, o.Lease)
	return e
}
