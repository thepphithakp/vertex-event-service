package port

import (
	"context"

	"github.com/vertex/event-service/internal/domain"
)

// ListFilter คุมขอบเขตการดึง event
//
// เดิม FindAll ดึงทั้งตารางเสมอ ตอนนี้มี 66 แถวจึงยังไม่เห็นปัญหา
// แต่ตารางนี้โตขึ้นเรื่อยๆ และไม่มีใครลบ — วันที่มีเป็นล้านแถว
// การดึงทั้งหมดจะทำให้ทั้ง database และ pod ล้มพร้อมกัน
type ListFilter struct {
	EntityType string
	EntityID   string
	ActorID    string

	// Limit จำกัดจำนวนแถวเสมอ ไม่มีทางเลือก "เอาทั้งหมด"
	Limit  int
	Offset int
}

type EventRepository interface {
	// Save เขียน event ลงฐานข้อมูล
	//
	// คืน created=false เมื่อ event นี้ถูกบันทึกไปแล้ว (idempotency key ซ้ำ)
	// ซึ่งไม่ใช่ error — เป็นผลลัพธ์ปกติของการ retry
	Save(ctx context.Context, event *domain.EventLog) (created bool, err error)

	List(ctx context.Context, f ListFilter) ([]domain.EventLog, error)
	Count(ctx context.Context, f ListFilter) (int64, error)
}

type EventUseCase interface {
	CreateEvent(ctx context.Context, event *domain.EventLog) (created bool, err error)
	ListEvents(ctx context.Context, f ListFilter) ([]domain.EventLog, int64, error)
}
