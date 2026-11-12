// Copyright (c) 2026 Beijing Volcano Engine Technology Co., Ltd. and/or its affiliates.
// SPDX-License-Identifier: Apache-2.0
package agentkit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"iter"
	"sort"
	"sync"

	"github.com/google/uuid"
	"google.golang.org/adk/v2/session"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type eventPosition struct {
	Position   uint64 `gorm:"primaryKey;autoIncrement"`
	SessionKey string `gorm:"size:64;uniqueIndex:session_event;index"`
	EventID    string `gorm:"size:256;uniqueIndex:session_event"`
}

func (eventPosition) TableName() string { return "veadk_session_event_positions" }

type orderedSessions struct {
	session.Service
	db    *gorm.DB
	locks [64]sync.Mutex
}

// NewOrderedSessionService adds a durable append-position index without changing
// ADK Event JSON or timestamps. ADK v2.1.0's database backend otherwise sorts by
// timestamp only. Pass the returned service to both the App and AIOLifecycle.
// AIO's shared lifecycle lease serializes writes across replicas. Existing events
// without an index retain their backend order; that historical order cannot be
// reconstructed retroactively when the backend discarded it.
func NewOrderedSessionService(service session.Service, db *gorm.DB) (session.Service, error) {
	if service == nil || db == nil {
		return nil, errors.New("session service and index database are required")
	}
	if err := db.AutoMigrate(&eventPosition{}); err != nil {
		return nil, err
	}
	return &orderedSessions{Service: service, db: db}, nil
}
func sessionIndexKey(app, user, id string) string {
	sum := sha256.Sum256([]byte(aioTrackKey(app, user, id)))
	return hex.EncodeToString(sum[:])
}
func (s *orderedSessions) Create(ctx context.Context, r *session.CreateRequest) (*session.CreateResponse, error) {
	// Storage validates uniqueness. Only clear obsolete positions after success.
	response, err := s.Service.Create(ctx, r)
	if err != nil {
		return nil, err
	}
	key := sessionIndexKey(response.Session.AppName(), response.Session.UserID(), response.Session.ID())
	if err := s.db.WithContext(ctx).Where("session_key = ?", key).Delete(&eventPosition{}).Error; err != nil {
		return nil, err
	}
	return response, nil
}
func (s *orderedSessions) AppendEvent(ctx context.Context, value session.Session, event *session.Event) error {
	if event == nil || value == nil {
		return errors.New("session and event are required")
	}
	key := sessionIndexKey(value.AppName(), value.UserID(), value.ID())
	lock := &s.locks[int(key[0])%len(s.locks)]
	lock.Lock()
	defer lock.Unlock()
	if event.ID == "" {
		event.ID = uuid.NewString()
	}
	// Reserve the position first. Failed/ambiguous ADK writes may leave an unused
	// position; Get joins only events actually present in ADK, so no phantom event
	// is exposed. Retrying the same ID keeps its original position.
	if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&eventPosition{SessionKey: key, EventID: event.ID}).Error; err != nil {
		return err
	}
	base := value
	if wrapped, ok := value.(*orderedSession); ok {
		base = wrapped.Session
	}
	if err := s.Service.AppendEvent(ctx, base, event); err != nil {
		return err
	}
	if wrapped, ok := value.(*orderedSession); ok && !event.Partial {
		wrapped.events = append(wrapped.events, event)
	}
	return nil
}
func (s *orderedSessions) Get(ctx context.Context, r *session.GetRequest) (*session.GetResponse, error) {
	// Apply ADK filtering after ordering, so NumRecentEvents refers to append order.
	query := *r
	query.NumRecentEvents = 0
	response, err := s.Service.Get(ctx, &query)
	if err != nil {
		return nil, err
	}
	var positions []eventPosition
	if err := s.db.WithContext(ctx).Where("session_key = ?", sessionIndexKey(r.AppName, r.UserID, r.SessionID)).Find(&positions).Error; err != nil {
		return nil, err
	}
	indexed := make(map[string]uint64, len(positions))
	for _, p := range positions {
		indexed[p.EventID] = p.Position
	}
	events := []*session.Event{}
	for e := range response.Session.Events().All() {
		events = append(events, e)
	}
	sort.SliceStable(events, func(i, j int) bool { a, b := indexed[events[i].ID], indexed[events[j].ID]; return a < b })
	if r.NumRecentEvents > 0 && len(events) > r.NumRecentEvents {
		events = events[len(events)-r.NumRecentEvents:]
	}
	response.Session = &orderedSession{Session: response.Session, events: events}
	return response, nil
}

// AppendEvent must unwrap this service's view before passing it to an ADK
// backend, which validates the concrete session type.
type orderedSession struct {
	session.Session
	events orderedEvents
}

func (s *orderedSession) Events() session.Events { return s.events }

type orderedEvents []*session.Event

func (e orderedEvents) Len() int                { return len(e) }
func (e orderedEvents) At(i int) *session.Event { return e[i] }
func (e orderedEvents) All() iter.Seq[*session.Event] {
	return func(yield func(*session.Event) bool) {
		for _, v := range e {
			if !yield(v) {
				return
			}
		}
	}
}

func (s *orderedSessions) Delete(ctx context.Context, r *session.DeleteRequest) error {
	if err := s.Service.Delete(ctx, r); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Where("session_key = ?", sessionIndexKey(r.AppName, r.UserID, r.SessionID)).Delete(&eventPosition{}).Error
}
