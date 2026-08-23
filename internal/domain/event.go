package domain

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// EventLog คือหนึ่งเหตุการณ์ที่ถูกบันทึกไว้
//
// ตารางนี้เป็น append-only — ไม่มี update และไม่มี delete จากโค้ด
type EventLog struct {
	ID uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`

	// Timestamp คือเวลาที่ "เหตุการณ์เกิด" ตามที่ service ต้นทางบอกมา
	Timestamp time.Time `gorm:"not null" json:"timestamp"`

	// CreatedAt คือเวลาที่ "ระบบบันทึก" ซึ่ง database เป็นคนใส่
	//
	// ต้องมีแยกจาก Timestamp เพราะผู้เรียกส่งค่าอะไรมาก็ได้
	// retention และการไล่ลำดับเหตุการณ์จริงจึงต้องอ้างอิงค่านี้
	CreatedAt time.Time `gorm:"not null;default:now()" json:"createdAt"`

	EventType     string `gorm:"not null" json:"eventType"`
	Action        string `gorm:"not null" json:"action"`
	ActorID       string `json:"actorId"`
	ActorUsername string `json:"actorUsername"`
	EntityID      string `json:"entityId"`
	EntityType    string `json:"entityType"`

	Payload datatypes.JSON `json:"payload"`

	// IdempotencyKey ให้ผู้ส่งกำหนดค่าที่แทน "เหตุการณ์นี้ครั้งนี้"
	//
	// ส่งซ้ำด้วยคีย์เดิมจะไม่เกิดแถวใหม่ ทำให้ retry ปลอดภัย
	// เป็น pointer เพราะต้องแยก "ไม่ได้ส่งมา" (NULL) ออกจาก "ส่งค่าว่าง"
	// — unique index ของ PostgreSQL ไม่นับ NULL ว่าซ้ำกัน
	IdempotencyKey *string `json:"idempotencyKey,omitempty"`
}

// TableName บอกชื่อตารางให้ชัด ไม่พึ่งการเดาพหูพจน์ของ GORM
func (EventLog) TableName() string { return "event_logs" }
