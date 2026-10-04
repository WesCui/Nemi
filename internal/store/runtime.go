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

func (s *Store) RunKind(ctx context.Context, r Ref) (string, error) {
	var kind string
	err := s.Pool.QueryRow(ctx, "SELECT kind FROM runs WHERE workspace_id=$1 AND id=$2", r.Workspace, r.ID).Scan(&kind)
	return kind, err
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
	var refs []byte
	e = tx.QueryRow(ctx, "SELECT snapshot_title,snapshot_source,model_profile,status,attempt_status,reserved_micro_cny,memory_refs FROM runs WHERE workspace_id=$1 AND id=$2 FOR UPDATE", r.Workspace, r.ID).Scan(&in.Title, &in.Source, &in.Profile, &status, &attempt, &in.Reservation, &refs)
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
	in.Source, _, e = memorySource(ctx, tx, r.Workspace, in.Source, refs)
	if e != nil {
		return in, e
	}
	in.Reservation = reserve(in.Title, in.Source)
	if in.Reservation > 1000000 {
		return in, errors.New("RUN_BUDGET_EXCEEDED")
	}
	var daily int64
	e = tx.QueryRow(ctx, "SELECT COALESCE(sum(reserved_micro_cny+charged_micro_cny),0) FROM runs WHERE workspace_id=$1 AND budget_day=(now() AT TIME ZONE 'Asia/Shanghai')::date", r.Workspace).Scan(&daily)
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
func (s *Store) ClaimModel(ctx context.Context, r Ref, expectedProfile ...string) (RunInput, error) {
	var in RunInput
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return in, e
	}
	defer tx.Rollback(ctx)
	var refs []byte
	e = tx.QueryRow(ctx, "UPDATE runs SET attempt_status='CALLING',updated_at=now() WHERE workspace_id=$1 AND id=$2 AND status='RUNNING' AND attempt_status='RESERVED' RETURNING snapshot_title,snapshot_source,model_profile,reserved_micro_cny,memory_refs", r.Workspace, r.ID).Scan(&in.Title, &in.Source, &in.Profile, &in.Reservation, &refs)
	if errors.Is(e, pgx.ErrNoRows) {
		return in, errors.New("MODEL_OUTCOME_UNKNOWN")
	}
	if e != nil {
		return in, e
	}
	// This transaction hasn't committed CALLING yet. A profile mismatch rolls
	// the claim back, proving that no model submission needs an unknown reserve.
	if len(expectedProfile) > 0 && in.Profile != expectedProfile[0] {
		return in, errors.New("MODEL_CONFIG_CHANGED")
	}
	var configID string
	if e = tx.QueryRow(ctx, "SELECT model_config_id FROM runs WHERE workspace_id=$1 AND id=$2", r.Workspace, r.ID).Scan(&configID); e != nil {
		return in, e
	}
	if configID != "" {
		var revoked bool
		e = tx.QueryRow(ctx, "SELECT revoked FROM personal_models WHERE workspace_id=$1 AND id=$2 FOR SHARE", r.Workspace, configID).Scan(&revoked)
		if errors.Is(e, pgx.ErrNoRows) || revoked {
			return in, errors.New("MODEL_CONFIG_REVOKED")
		}
		if e != nil {
			return in, e
		}
	}
	var count int
	in.Source, count, e = memorySource(ctx, tx, r.Workspace, in.Source, refs)
	if e != nil {
		return in, e
	}
	if _, e = tx.Exec(ctx, "UPDATE runs SET used_memory_count=$3 WHERE workspace_id=$1 AND id=$2", r.Workspace, r.ID, count); e != nil {
		return in, e
	}
	return in, tx.Commit(ctx)
}
func (s *Store) FinishRun(ctx context.Context, r Ref, plan domain.Plan, input, output, cost int64) error {
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var status, mid, kind, conversation string
	var rev int
	e = tx.QueryRow(ctx, "SELECT status,COALESCE(matter_id,''),matter_revision,kind,COALESCE(conversation_id,'') FROM runs WHERE workspace_id=$1 AND id=$2 FOR UPDATE", r.Workspace, r.ID).Scan(&status, &mid, &rev, &kind, &conversation)
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
	if _, e = tx.Exec(ctx, "UPDATE personal_models SET verified_at=now() WHERE workspace_id=$1 AND id=(SELECT model_config_id FROM runs WHERE workspace_id=$1 AND id=$2) AND NOT revoked", r.Workspace, r.ID); e != nil {
		return e
	}
	if kind == "chat" {
		_, e = tx.Exec(ctx, "UPDATE conversations SET updated_at=now() WHERE workspace_id=$1 AND id=$2", r.Workspace, conversation)
	} else {
		// Never overwrite a user's newer checklist.
		_, e = tx.Exec(ctx, "UPDATE matters SET items=$3,revision=revision+1 WHERE workspace_id=$1 AND id=$2 AND revision=$4 AND status='ACTIVE'", r.Workspace, mid, ib, rev)
	}
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
	if e = failRunTx(ctx, tx, r, code, input, output, cost); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func failRunTx(ctx context.Context, tx pgx.Tx, r Ref, code string, input, output, cost int64) error {
	// A completed activity result may have committed just before the workflow timed out.
	tag, e := tx.Exec(ctx, `UPDATE runs SET status='FAILED',error_code=$3,
 attempt_status=CASE WHEN (kind='plan' AND attempt_status='CALLING' AND $4=0) OR EXISTS(SELECT 1 FROM agent_steps WHERE workspace_id=$1 AND run_id=$2 AND kind='MODEL' AND status IN ('CALLING','UNKNOWN')) THEN 'UNKNOWN' ELSE 'SETTLED' END,
 reserved_micro_cny=CASE WHEN (kind='plan' AND attempt_status='CALLING' AND $4=0) OR EXISTS(SELECT 1 FROM agent_steps WHERE workspace_id=$1 AND run_id=$2 AND kind='MODEL' AND status IN ('CALLING','UNKNOWN')) THEN reserved_micro_cny ELSE 0 END,
 charged_micro_cny=GREATEST(charged_micro_cny,$4),input_tokens=GREATEST(input_tokens,$5),output_tokens=GREATEST(output_tokens,$6),updated_at=now()
 WHERE workspace_id=$1 AND id=$2 AND status IN ('QUEUED','RUNNING')`, r.Workspace, r.ID, code, cost, input, output)
	if e != nil {
		return e
	}
	if tag.RowsAffected() > 0 {
		if e = event(ctx, tx, r.Workspace, "run.failed", r.ID); e != nil {
			return e
		}
	}
	return nil
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
	var due, nominal time.Time
	var until *time.Time
	var repeat string
	var quiet bool
	var mid, title, status string
	var deadline *time.Time
	// Match edit/complete lock order: matter first, then reminder. This prevents
	// a delivery racing a completion from acquiring those rows in reverse order.
	e = tx.QueryRow(ctx, "SELECT matter_id FROM reminders WHERE workspace_id=$1 AND id=$2", r.Workspace, r.ID).Scan(&mid)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil
	}
	if e != nil {
		return e
	}
	if e = tx.QueryRow(ctx, "SELECT title,status,deadline FROM matters WHERE workspace_id=$1 AND id=$2 FOR UPDATE", r.Workspace, mid).Scan(&title, &status, &deadline); e != nil {
		return e
	}
	e = tx.QueryRow(ctx, "SELECT enabled,revision,due_at,nominal_at,quiet,repeat,repeat_until FROM reminders WHERE workspace_id=$1 AND id=$2 FOR UPDATE", r.Workspace, r.ID).Scan(&enabled, &rev, &due, &nominal, &quiet, &repeat, &until)
	if e != nil {
		return e
	}
	if !enabled || rev != revision || status != "ACTIVE" {
		return nil
	}
	now := time.Now()
	if now.Before(due) {
		return errors.New("REMINDER_NOT_DUE")
	}
	deliveryStatus := "AVAILABLE"
	if now.Sub(due) > 10*time.Minute {
		deliveryStatus = "OVERDUE"
	}
	tag, e := tx.Exec(ctx, "INSERT INTO notifications(workspace_id,id,reminder_id,revision,matter_id,title,status) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(workspace_id,reminder_id,revision) DO NOTHING", r.Workspace, domain.ID(), r.ID, revision, mid, title, deliveryStatus)
	if e != nil {
		return e
	}
	next, e := domain.NextOccurrence(nominal, until, repeat, quiet, now)
	if e != nil {
		return e
	}
	if next != nil && deadline != nil && domain.EffectiveDue(*next, quiet).After(*deadline) {
		next = nil
	}
	if next != nil {
		nextDue := domain.EffectiveDue(*next, quiet)
		_, e = tx.Exec(ctx, "UPDATE reminders SET revision=revision+1,nominal_at=$3,due_at=$4,sync_status='PENDING_SYNC' WHERE workspace_id=$1 AND id=$2", r.Workspace, r.ID, *next, nextDue)
		if e != nil {
			return e
		}
		if e = enqueue(ctx, tx, r.Workspace, "reminder", r.ID, rev+1, &nextDue); e != nil {
			return e
		}
	} else if repeat != "once" {
		_, e = tx.Exec(ctx, "UPDATE reminders SET revision=revision+1,enabled=false,sync_status='ENDED' WHERE workspace_id=$1 AND id=$2", r.Workspace, r.ID)
		if e != nil {
			return e
		}
		if e = event(ctx, tx, r.Workspace, "reminder.ended", r.ID); e != nil {
			return e
		}
	} else {
		if _, e = tx.Exec(ctx, "UPDATE reminders SET sync_status='FIRED' WHERE workspace_id=$1 AND id=$2", r.Workspace, r.ID); e != nil {
			return e
		}
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

func (s *Store) LeaseOutbox(ctx context.Context, workspaces ...string) (*Outbox, error) {
	o := Outbox{Lease: domain.ID()}
	filter := ""
	args := []any{o.Lease}
	if len(workspaces) > 0 && workspaces[0] != "" {
		filter = " AND workspace_id=$2"
		args = append(args, workspaces[0])
	}
	query := `UPDATE outbox SET leased_until=now()+interval '30 seconds',lease_token=$1,attempts=attempts+1 WHERE id=(
 SELECT id FROM outbox WHERE state='PENDING' AND available_at<=now() AND (leased_until IS NULL OR leased_until<now())` + filter + ` ORDER BY available_at,id FOR UPDATE SKIP LOCKED LIMIT 1
 ) RETURNING id,workspace_id,kind,subject_id,revision,due_at`
	e := s.Pool.QueryRow(ctx, query, args...).Scan(&o.ID, &o.Workspace, &o.Kind, &o.Subject, &o.Revision, &o.Due)
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
