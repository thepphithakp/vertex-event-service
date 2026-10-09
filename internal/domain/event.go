package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// EventLog คือหนึ่งเหตุการณ์ที่ถูกบันทึกไว้
//
// ตารางนี้เป็น append-only — ไม่มี update และไม่มี delete จากโค้ด
//
// 🔴 type นี้เป็น domain ล้วน ไม่มี gorm/json tag — เดิมเป็นทั้ง GORM model
// (มี gorm tag + TableName()) และ API request/response DTO (มี json tag,
// parse ตรงจาก BodyParser) ในตัวเดียว ย้าย GORM ไป
// adapter/repository/model (EventLogRow) และ wire format ไป adapter/handler
// (createEventRequest/eventLogResponse) แทน
type EventLog struct {
	ID uuid.UUID

	// Timestamp คือเวลาที่ "เหตุการณ์เกิด" ตามที่ service ต้นทางบอกมา
	Timestamp time.Time

	// CreatedAt คือเวลาที่ "ระบบบันทึก" ซึ่ง database เป็นคนใส่
	//
	// ต้องมีแยกจาก Timestamp เพราะผู้เรียกส่งค่าอะไรมาก็ได้
	// retention และการไล่ลำดับเหตุการณ์จริงจึงต้องอ้างอิงค่านี้
	CreatedAt time.Time

	EventType     string
	Action        string
	ActorID       string
	ActorUsername string
	EntityID      string
	EntityType    string

	// Payload เป็น json.RawMessage (stdlib) ไม่ใช่ gorm.io/datatypes.JSON —
	// เนื้อหายังเป็น JSON ดิบผ่านๆ เหมือนเดิมทุกประการ (ไม่มีใครใน service
	// นี้ตรวจรูปร่างข้างในเลย มีแต่ len() วัดขนาด) แค่ไม่ผูกกับ GORM
	Payload json.RawMessage

	// IdempotencyKey ให้ผู้ส่งกำหนดค่าที่แทน "เหตุการณ์นี้ครั้งนี้"
	//
	// ส่งซ้ำด้วยคีย์เดิมจะไม่เกิดแถวใหม่ ทำให้ retry ปลอดภัย
	// เป็น pointer เพราะต้องแยก "ไม่ได้ส่งมา" (NULL) ออกจาก "ส่งค่าว่าง"
	// — unique index ของ PostgreSQL ไม่นับ NULL ว่าซ้ำกัน
	IdempotencyKey *string
}
