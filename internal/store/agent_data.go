package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"nemi/internal/domain"
)

type ChecklistChange struct {
	Index  *int    `json:"index"`
	Done   *bool   `json:"done,omitempty"`
	Text   *string `json:"text,omitempty"`
	Remove bool    `json:"remove,omitempty"`
}
type MatterChange struct {
	ID string `json:"matter_id"`
	EditMatter
	Checklist []ChecklistChange `json:"checklist_changes,omitempty"`
	Append    []domain.Item     `json:"append_items,omitempty"`
}
type ReminderChange struct {
	ID             string     `json:"matter_id"`
	MatterRevision int        `json:"matter_revision"`
	Expected       int        `json:"expected_revision"`
	At             *time.Time `json:"at,omitempty"`
	Enabled        *bool      `json:"enabled"`
	Quiet          *bool      `json:"quiet,omitempty"`
	Repeat         string     `json:"repeat,omitempty"`
	Until          *time.Time `json:"repeat_until,omitempty"`
}
type MemoryChange struct {
	ID        string `json:"memory_id"`
	Operation string `json:"operation"`
	Expected  int    `json:"expected_revision"`
	Category  string `json:"category"`
	Text      string `json:"text"`
}
type DataProposal struct {
	Title            string           `json:"title"`
	Matter           *domain.Matter   `json:"before_matter,omitempty"`
	Reminder         *domain.Reminder `json:"before_reminder,omitempty"`
	Memory           *domain.Memory   `json:"before_memory,omitempty"`
	MatterPatch      *EditMatter      `json:"matter_patch,omitempty"`
	ReminderPatch    *SaveReminder    `json:"reminder_patch,omitempty"`
	MemoryPatch      *SaveMemory      `json:"memory_patch,omitempty"`
	ReminderRevision int              `json:"reminder_revision"`
}

func reminderSnapshot(ctx context.Context, tx pgx.Tx, w, id string) (*domain.Reminder, error) {
	var r domain.Reminder
	err := tx.QueryRow(ctx, `SELECT id,matter_id,revision,nominal_at,due_at,quiet,enabled,sync_status,repeat,repeat_until FROM reminders WHERE workspace_id=$1 AND matter_id=$2 FOR SHARE`, w, id).Scan(&r.ID, &r.MatterID, &r.Revision, &r.Nominal, &r.Due, &r.Quiet, &r.Enabled, &r.SyncStatus, &r.Repeat, &r.Until)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &r, err
}
func (s *Store) AgentReminder(ctx context.Context, w, id string) (any, error) {
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	m, err := scanMatter(tx.QueryRow(ctx, matterSelect+" WHERE workspace_id=$1 AND id=$2 FOR SHARE", w, id))
	if err != nil {
		return nil, err
	}
	r, err := reminderSnapshot(ctx, tx, w, id)
	if err != nil {
		return nil, err
	}
	return map[string]any{"matter_id": id, "title": m.Title, "matter_revision": m.Revision, "deadline": m.Deadline, "matter_status": m.Status, "reminder": r}, nil
}

func (s *Store) dataProposal(ctx context.Context, r Ref, kind string, input any, build func(pgx.Tx) (DataProposal, error)) (any, error) {
	canonical, _ := json.Marshal(input)
	hash := sha256.Sum256(append([]byte(r.ID+":"+kind+":"), canonical...))
	result, err := s.Command(ctx, r.Workspace, hex.EncodeToString(hash[:]), "agent/data/"+kind, canonical, func(tx pgx.Tx) (any, int, error) {
		if _, err := activeChat(ctx, tx, r); err != nil {
			return nil, 0, err
		}
		payload, err := build(tx)
		if err != nil {
			return nil, 0, err
		}
		body, err := json.Marshal(payload)
		if err != nil {
			return nil, 0, err
		}
		id := domain.ID()
		if _, err = tx.Exec(ctx, "INSERT INTO agent_actions(workspace_id,id,run_id,kind,payload) VALUES($1,$2,$3,$4,$5)", r.Workspace, id, r.ID, kind, body); err != nil {
			return nil, 0, err
		}
		if err = event(ctx, tx, r.Workspace, "agent.action_pending", r.ID); err != nil {
			return nil, 0, err
		}
		// Full before/after data stays in the review card, not a paid tool result.
		return map[string]string{"id": id, "kind": kind, "title": payload.Title, "status": "PENDING"}, 201, nil
	})
	if err != nil {
		return nil, err
	}
	var out any
	err = json.Unmarshal(result.Body, &out)
	return out, err
}
func (s *Store) ProposeMatterChange(ctx context.Context, r Ref, p MatterChange) (any, error) {
	if len(p.ID) != 32 || p.Items != nil {
		return nil, errors.New("INVALID_ARGUMENTS")
	}
	return s.dataProposal(ctx, r, "update_matter", p, func(tx pgx.Tx) (DataProposal, error) {
		m, err := scanMatter(tx.QueryRow(ctx, matterSelect+" WHERE workspace_id=$1 AND id=$2 FOR SHARE", r.Workspace, p.ID))
		if err != nil {
			return DataProposal{}, err
		}
		if m.Revision != p.Expected {
			return DataProposal{}, domain.ErrConflict
		}
		if len(p.Checklist) > 0 || len(p.Append) > 0 {
			if p.Items != nil || len(p.Append) > 8 || len(p.Checklist) > 8 {
				return DataProposal{}, errors.New("INVALID_ARGUMENTS")
			}
			items := append([]domain.Item{}, m.Items...)
			seen := map[int]bool{}
			remove := map[int]bool{}
			for _, c := range p.Checklist {
				if c.Index == nil || *c.Index < 0 || *c.Index >= len(items) || seen[*c.Index] || (c.Remove && (c.Done != nil || c.Text != nil)) || (!c.Remove && c.Done == nil && c.Text == nil) {
					return DataProposal{}, errors.New("INVALID_ARGUMENTS")
				}
				index := *c.Index
				seen[index] = true
				if c.Remove {
					remove[index] = true
				}
				if c.Done != nil {
					items[index].Done = *c.Done
				}
				if c.Text != nil {
					items[index].Text = *c.Text
				}
			}
			next := []domain.Item{}
			for i, item := range items {
				if !remove[i] {
					next = append(next, item)
				}
			}
			next = append(next, p.Append...)
			p.Items = &next
		}
		if err = p.EditMatter.Validate(time.Now()); err != nil {
			return DataProposal{}, err
		}
		rem, err := reminderSnapshot(ctx, tx, r.Workspace, m.ID)
		if err != nil {
			return DataProposal{}, err
		}
		if p.Deadline != nil && rem != nil && rem.Enabled && (rem.Due.After(*p.Deadline) || (rem.Until != nil && rem.Until.After(*p.Deadline))) {
			return DataProposal{}, errors.New("ACTION_INVALID")
		}
		proposal := DataProposal{Title: "修改事项：" + m.Title, Matter: &m, Reminder: rem, MatterPatch: &p.EditMatter}
		if rem != nil {
			proposal.ReminderRevision = rem.Revision
		}
		if p.Status != nil && *p.Status == "ARCHIVED" {
			proposal.Title = "归档事项：" + m.Title
		}
		return proposal, nil
	})
}
func (s *Store) ProposeReminderChange(ctx context.Context, r Ref, p ReminderChange) (any, error) {
	if len(p.ID) != 32 || p.MatterRevision < 1 || p.Expected < 0 || p.Enabled == nil {
		return nil, errors.New("INVALID_ARGUMENTS")
	}
	return s.dataProposal(ctx, r, "update_reminder", p, func(tx pgx.Tx) (DataProposal, error) {
		m, err := scanMatter(tx.QueryRow(ctx, matterSelect+" WHERE workspace_id=$1 AND id=$2 FOR SHARE", r.Workspace, p.ID))
		if err != nil {
			return DataProposal{}, err
		}
		if m.Revision != p.MatterRevision || m.Status != "ACTIVE" {
			return DataProposal{}, domain.ErrConflict
		}
		before, err := reminderSnapshot(ctx, tx, r.Workspace, p.ID)
		if err != nil {
			return DataProposal{}, err
		}
		rev := 0
		if before != nil {
			rev = before.Revision
		}
		if rev != p.Expected {
			return DataProposal{}, domain.ErrConflict
		}
		if !*p.Enabled && before == nil {
			return DataProposal{}, errors.New("INVALID_ARGUMENTS")
		}
		next := SaveReminder{Expected: p.Expected, At: p.At, Enabled: *p.Enabled, Timezone: "Asia/Shanghai", Confirmed: true, Repeat: domain.NormalizeRepeat(p.Repeat), Until: p.Until}
		if before != nil {
			next.Quiet = before.Quiet
		}
		if p.Quiet != nil {
			next.Quiet = *p.Quiet
		}
		if p.Repeat == "" && before != nil {
			next.Repeat = before.Repeat
			if p.Until == nil {
				next.Until = before.Until
			}
		}
		if next.At == nil && before != nil {
			at := before.Nominal
			next.At = &at
		}
		if !next.Enabled {
			at := before.Nominal
			next.At = &at
			next.Quiet = before.Quiet
			next.Repeat = before.Repeat
			next.Until = before.Until
		}
		if err = validateReminder(next, m.Deadline, time.Now()); err != nil {
			return DataProposal{}, err
		}
		return DataProposal{Title: "调整站内提醒：" + m.Title, Matter: &m, Reminder: before, ReminderPatch: &next, ReminderRevision: rev}, nil
	})
}
func validateReminder(p SaveReminder, deadline *time.Time, now time.Time) error {
	if !p.Confirmed || p.Timezone != "Asia/Shanghai" || p.At == nil {
		return errors.New("ACTION_INVALID")
	}
	if p.Enabled && domain.ValidateRecurrence(*p.At, p.Until, p.Repeat, p.Quiet, deadline, now) != nil {
		return errors.New("ACTION_INVALID")
	}
	return nil
}
func (s *Store) ProposeMemoryChange(ctx context.Context, r Ref, p MemoryChange) (any, error) {
	if (p.ID == "" && p.Expected != 0) || (p.ID != "" && (len(p.ID) != 32 || p.Expected < 1)) || (p.Operation != "save" && p.Operation != "delete") || (p.Operation == "delete" && p.ID == "") {
		return nil, errors.New("INVALID_ARGUMENTS")
	}
	kind := "save_memory"
	if p.Operation == "delete" {
		kind = "delete_memory"
	}
	return s.dataProposal(ctx, r, kind, p, func(tx pgx.Tx) (DataProposal, error) {
		proposal := DataProposal{Title: "保存生活偏好"}
		if p.ID != "" {
			var m domain.Memory
			err := tx.QueryRow(ctx, "SELECT id,category,content,revision,updated_at FROM memories WHERE workspace_id=$1 AND id=$2 FOR SHARE", r.Workspace, p.ID).Scan(&m.ID, &m.Category, &m.Text, &m.Revision, &m.UpdatedAt)
			if errors.Is(err, pgx.ErrNoRows) {
				return proposal, domain.ErrNotFound
			}
			if err != nil {
				return proposal, err
			}
			if m.Revision != p.Expected {
				return proposal, domain.ErrConflict
			}
			proposal.Memory = &m
		}
		if p.Operation == "delete" {
			proposal.Title = "移除生活偏好"
			return proposal, nil
		}
		input := SaveMemory{Expected: p.Expected, Category: p.Category, Text: p.Text, Confirmed: true}
		if err := input.Validate(); err != nil {
			return proposal, errors.New("INVALID_ARGUMENTS")
		}
		proposal.MemoryPatch = &input
		if proposal.Memory != nil {
			proposal.Title = "修改生活偏好"
		}
		return proposal, nil
	})
}
func DataAction(kind string) bool {
	return kind == "update_matter" || kind == "update_reminder" || kind == "save_memory" || kind == "delete_memory"
}
func (s *Store) ApplyDataAction(ctx context.Context, tx pgx.Tx, w string, a domain.AgentAction) (string, error) {
	var p DataProposal
	if json.Unmarshal(a.Payload, &p) != nil {
		return "", errors.New("ACTION_INVALID")
	}
	switch a.Kind {
	case "update_matter", "update_reminder":
		if p.Matter == nil {
			return "", errors.New("ACTION_INVALID")
		}
		m, err := scanMatter(tx.QueryRow(ctx, matterSelect+" WHERE workspace_id=$1 AND id=$2 FOR UPDATE", w, p.Matter.ID))
		if err != nil {
			return "", err
		}
		if m.Revision != p.Matter.Revision {
			return "", domain.ErrConflict
		}
		var rev int
		err = tx.QueryRow(ctx, "SELECT revision FROM reminders WHERE workspace_id=$1 AND matter_id=$2 FOR UPDATE", w, m.ID).Scan(&rev)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return "", err
		}
		if rev != p.ReminderRevision {
			return "", domain.ErrConflict
		}
		if a.Kind == "update_matter" {
			if p.MatterPatch == nil {
				return "", errors.New("ACTION_INVALID")
			}
			_, _, err = s.EditMatter(ctx, tx, w, m.ID, *p.MatterPatch)
		} else {
			if p.ReminderPatch == nil || validateReminder(*p.ReminderPatch, m.Deadline, time.Now()) != nil {
				return "", errors.New("ACTION_INVALID")
			}
			_, _, err = s.SaveReminder(ctx, tx, w, m.ID, *p.ReminderPatch)
		}
		return m.ID, err
	case "save_memory":
		if p.MemoryPatch == nil || p.MemoryPatch.Validate() != nil {
			return "", errors.New("ACTION_INVALID")
		}
		id := ""
		if p.Memory != nil {
			id = p.Memory.ID
		}
		out, _, err := s.SaveMemory(ctx, tx, w, id, *p.MemoryPatch)
		if err != nil {
			return "", err
		}
		return out.(map[string]any)["id"].(string), nil
	case "delete_memory":
		if p.Memory == nil {
			return "", errors.New("ACTION_INVALID")
		}
		_, _, err := s.DeleteMemory(ctx, tx, w, p.Memory.ID, p.Memory.Revision)
		return p.Memory.ID, err
	}
	return "", errors.New("ACTION_INVALID")
}
