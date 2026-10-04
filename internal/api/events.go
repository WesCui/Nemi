package api

import (
	"context"
	"nemi/internal/domain"
	"nemi/internal/store"
	"sync"
	"time"
)

type subscription struct {
	workspace string
	events    chan domain.Event
}
type Hub struct {
	mu    sync.Mutex
	subs  map[*subscription]struct{}
	store *store.Store
}

func NewHub(s *store.Store) *Hub { return &Hub{subs: map[*subscription]struct{}{}, store: s} }
func (h *Hub) subscribe(w string) (*subscription, func()) {
	s := &subscription{w, make(chan domain.Event, 32)}
	h.mu.Lock()
	h.subs[s] = struct{}{}
	h.mu.Unlock()
	return s, func() {
		h.mu.Lock()
		if _, ok := h.subs[s]; ok {
			delete(h.subs, s)
			close(s.events)
		}
		h.mu.Unlock()
	}
}

// Exactly one database reader per API process, independent of SSE connection count.
func (h *Hub) Run(ctx context.Context) {
	var cursor int64
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			rows, e := h.store.Pool.Query(ctx, "SELECT sequence,workspace_id,kind,subject_id,created_at FROM business_events WHERE sequence>$1 ORDER BY sequence LIMIT 200", cursor)
			if e != nil {
				continue
			}
			for rows.Next() {
				var ev domain.Event
				var w string
				if e = rows.Scan(&ev.Sequence, &w, &ev.Kind, &ev.SubjectID, &ev.CreatedAt); e != nil {
					break
				}
				cursor = ev.Sequence
				h.mu.Lock()
				for s := range h.subs {
					if s.workspace == w {
						select {
						case s.events <- ev:
						default:
							close(s.events)
							delete(h.subs, s)
						}
					}
				}
				h.mu.Unlock()
			}
			rows.Close()
		}
	}
}
