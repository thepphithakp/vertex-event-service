// Package model เก็บ struct ที่ผูกกับ GORM/schema โดยตรง — ไม่มีใครนอก
// adapter/repository เห็น struct พวกนี้เลย แปลงเป็น domain type ที่ชายแดน
// ด้วย ToDomain()/FromDomain() ตามแบบของ vertex-auth-service
package model

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	"github.com/vertex/event-service/internal/domain"
)

type EventLogRow struct {
	ID uuid.UUID `gorm:"type:uuid;primaryKey"`

	Timestamp time.Time `gorm:"not null"`
	CreatedAt time.Time `gorm:"not null;default:now()"`

	EventType     string `gorm:"not null"`
	Action        string `gorm:"not null"`
	ActorID       string
	ActorUsername string
	EntityID      string
	EntityType    string

	Payload datatypes.JSON

	IdempotencyKey *string
}

// TableName บอกชื่อตารางให้ชัด ไม่พึ่งการเดาพหูพจน์ของ GORM
func (EventLogRow) TableName() string { return "event_logs" }

// ToDomain/FromDomain ไม่มี error เพราะ json.RawMessage กับ datatypes.JSON
// เป็น []byte ทั้งคู่ — แค่ cast type ไม่มี marshal/unmarshal ที่พังได้

func (r EventLogRow) ToDomain() domain.EventLog {
	return domain.EventLog{
		ID:             r.ID,
		Timestamp:      r.Timestamp,
		CreatedAt:      r.CreatedAt,
		EventType:      r.EventType,
		Action:         r.Action,
		ActorID:        r.ActorID,
		ActorUsername:  r.ActorUsername,
		EntityID:       r.EntityID,
		EntityType:     r.EntityType,
		Payload:        json.RawMessage(r.Payload),
		IdempotencyKey: r.IdempotencyKey,
	}
}

func EventLogRowFromDomain(e domain.EventLog) EventLogRow {
	return EventLogRow{
		ID:             e.ID,
		Timestamp:      e.Timestamp,
		CreatedAt:      e.CreatedAt,
		EventType:      e.EventType,
		Action:         e.Action,
		ActorID:        e.ActorID,
		ActorUsername:  e.ActorUsername,
		EntityID:       e.EntityID,
		EntityType:     e.EntityType,
		Payload:        datatypes.JSON(e.Payload),
		IdempotencyKey: e.IdempotencyKey,
	}
}
