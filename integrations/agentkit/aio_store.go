// Copyright (c) 2026 Beijing Volcano Engine Technology Co., Ltd. and/or its affiliates.
// SPDX-License-Identifier: Apache-2.0
package agentkit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// AIOLifecycleRecord is internal orchestration state, never an ADK REST field.
// Persist it in the same deployment as the ADK session database.
type AIOLifecycleRecord struct {
	App          string
	User         string
	Session      string
	LastActivity time.Time
	PausedAt     time.Time
	Deleted      bool
	PendingFiles []string
}

// AIOLifecycleStore coordinates every foreground and background operation.
// Claim/Renew/Save/Release must compare owner atomically. Save must reject an
// expired lease. All replicas serving the same sessions must use the same store.
type AIOLifecycleStore interface {
	List(context.Context) ([]AIOLifecycleRecord, error)
	Claim(context.Context, string, string, string, string, time.Duration) (AIOLifecycleRecord, error)
	Renew(context.Context, string, string, string, string, time.Duration) error
	Save(context.Context, string, AIOLifecycleRecord) error
	Release(context.Context, string, string, string, string) error
}

type aioLifecycleRow struct {
	Key     string `gorm:"primaryKey;column:record_key;size:64"`
	Payload string `gorm:"type:text;not null"`
	Owner   string `gorm:"size:64;not null"`
	Expires int64  `gorm:"not null"`
}

func (aioLifecycleRow) TableName() string { return "veadk_aio_lifecycle" }
func lifecycleKey(app, user, id string) string {
	sum := sha256.Sum256([]byte(aioTrackKey(app, user, id)))
	return hex.EncodeToString(sum[:])
}

type gormLifecycleStore struct{ db *gorm.DB }

// NewGormAIOLifecycleStore creates the durable registry/lease table using the
// application's SQLite, PostgreSQL or MySQL connection. For multiple hosts use
// a shared database, not separate local SQLite files. Database clocks/host clocks
// must be synchronized for lease expiry; owner tokens fence stale writes.
func NewGormAIOLifecycleStore(db *gorm.DB) (AIOLifecycleStore, error) {
	if db == nil {
		return nil, errors.New("lifecycle database is required")
	}
	if err := db.AutoMigrate(&aioLifecycleRow{}); err != nil {
		return nil, err
	}
	return &gormLifecycleStore{db}, nil
}
func (s *gormLifecycleStore) List(ctx context.Context) ([]AIOLifecycleRecord, error) {
	var rows []aioLifecycleRow
	if err := s.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, err
	}
	records := make([]AIOLifecycleRecord, 0, len(rows))
	for _, row := range rows {
		var r AIOLifecycleRecord
		if err := json.Unmarshal([]byte(row.Payload), &r); err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, nil
}
func (s *gormLifecycleStore) Claim(ctx context.Context, app, user, id, owner string, ttl time.Duration) (AIOLifecycleRecord, error) {
	record := AIOLifecycleRecord{App: app, User: user, Session: id}
	data, _ := json.Marshal(record)
	key := lifecycleKey(app, user, id)
	db := s.db.WithContext(ctx)
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&aioLifecycleRow{Key: key, Payload: string(data)}).Error; err != nil {
		return record, err
	}
	now := time.Now()
	result := db.Model(&aioLifecycleRow{}).Where("record_key = ? AND (owner = ? OR expires <= ?)", key, "", now.UnixNano()).Updates(map[string]any{"owner": owner, "expires": now.Add(ttl).UnixNano()})
	if result.Error != nil {
		return record, result.Error
	}
	if result.RowsAffected != 1 {
		return record, ErrAIOSessionBusy
	}
	var row aioLifecycleRow
	if err := db.Where("record_key = ? AND owner = ?", key, owner).First(&row).Error; err != nil {
		return record, err
	}
	if err := json.Unmarshal([]byte(row.Payload), &record); err != nil {
		return record, err
	}
	return record, nil
}
func (s *gormLifecycleStore) Renew(ctx context.Context, app, user, id, owner string, ttl time.Duration) error {
	now := time.Now()
	r := s.db.WithContext(ctx).Model(&aioLifecycleRow{}).Where("record_key = ? AND owner = ? AND expires > ?", lifecycleKey(app, user, id), owner, now.UnixNano()).Update("expires", now.Add(ttl).UnixNano())
	return leaseResult(r)
}
func (s *gormLifecycleStore) Save(ctx context.Context, owner string, record AIOLifecycleRecord) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	r := s.db.WithContext(ctx).Model(&aioLifecycleRow{}).Where("record_key = ? AND owner = ? AND expires > ?", lifecycleKey(record.App, record.User, record.Session), owner, time.Now().UnixNano()).Update("payload", string(data))
	return leaseResult(r)
}
func (s *gormLifecycleStore) Release(ctx context.Context, app, user, id, owner string) error {
	r := s.db.WithContext(ctx).Model(&aioLifecycleRow{}).Where("record_key = ? AND owner = ?", lifecycleKey(app, user, id), owner).Updates(map[string]any{"owner": "", "expires": 0})
	return leaseResult(r)
}
func leaseResult(r *gorm.DB) error {
	if r.Error != nil {
		return r.Error
	}
	if r.RowsAffected != 1 {
		return errors.New("AIO lifecycle lease lost")
	}
	return nil
}
