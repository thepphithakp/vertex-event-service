package application

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/vertex/event-service/internal/domain"
	"github.com/vertex/event-service/internal/port"
)

// ขอบเขตของค่าที่ยอมรับ — กันไม่ให้ผู้เรียกเขียนข้อมูลขยะลงตารางที่ลบไม่ได้
const (
	maxFieldLength   = 256
	maxPayloadBytes  = 16 << 10 // 16KB
	defaultPageLimit = 50
	maxPageLimit     = 500
)

// futureSkew เผื่อเวลาที่นาฬิกาของ service ต้นทางเดินไม่ตรงกัน
//
// ไม่ยอมรับ event ที่อ้างว่าเกิดในอนาคตไกลกว่านี้ ไม่งั้นแถวเดียว
// ที่ timestamp ผิดจะไปค้างอยู่บนสุดของ timeline ตลอดกาล
const futureSkew = 5 * time.Minute

type EventService struct {
	repo port.EventRepository
	now  func() time.Time
}

func NewEventService(repo port.EventRepository) *EventService {
	return &EventService{repo: repo, now: time.Now}
}

func (s *EventService) CreateEvent(ctx context.Context, event *domain.EventLog) (bool, error) {
	if err := s.normalize(event); err != nil {
		return false, err
	}
	return s.repo.Save(ctx, event)
}

// normalize เติมค่าที่ขาดและปฏิเสธค่าที่ใช้ไม่ได้
//
// ตรวจที่ชั้นนี้เพราะเป็นกฎของโดเมน ไม่ใช่เรื่องของ HTTP
// database มี NOT NULL คุมอยู่อีกชั้นแล้ว แต่การปล่อยให้ไปล้มที่ database
// จะได้ error ที่ผู้เรียกอ่านไม่รู้เรื่องและกลายเป็น 500 แทนที่จะเป็น 400
func (s *EventService) normalize(e *domain.EventLog) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = s.now()
	}
	if e.Timestamp.After(s.now().Add(futureSkew)) {
		return &ValidationError{Field: "timestamp", Reason: "อยู่ในอนาคตไกลเกินไป"}
	}

	e.EventType = strings.TrimSpace(e.EventType)
	e.Action = strings.TrimSpace(e.Action)

	if e.EventType == "" {
		return &ValidationError{Field: "eventType", Reason: "ต้องไม่ว่าง"}
	}
	if e.Action == "" {
		return &ValidationError{Field: "action", Reason: "ต้องไม่ว่าง"}
	}

	fields := map[string]string{
		"eventType":     e.EventType,
		"action":        e.Action,
		"actorId":       e.ActorID,
		"actorUsername": e.ActorUsername,
		"entityId":      e.EntityID,
		"entityType":    e.EntityType,
	}
	for name, v := range fields {
		if len(v) > maxFieldLength {
			return &ValidationError{Field: name, Reason: fmt.Sprintf("ยาวเกิน %d ตัวอักษร", maxFieldLength)}
		}
	}

	if len(e.Payload) > maxPayloadBytes {
		return &ValidationError{Field: "payload", Reason: fmt.Sprintf("ใหญ่เกิน %d ไบต์", maxPayloadBytes)}
	}

	if e.IdempotencyKey != nil {
		k := strings.TrimSpace(*e.IdempotencyKey)
		switch {
		case k == "":
			// ส่งค่าว่างมาถือว่าไม่ได้ส่ง — ไม่งั้นแถวที่สองที่ส่งค่าว่างจะชน unique index
			e.IdempotencyKey = nil
		case len(k) > maxFieldLength:
			return &ValidationError{Field: "idempotencyKey", Reason: fmt.Sprintf("ยาวเกิน %d ตัวอักษร", maxFieldLength)}
		default:
			e.IdempotencyKey = &k
		}
	}

	return nil
}

func (s *EventService) ListEvents(ctx context.Context, f port.ListFilter) ([]domain.EventLog, int64, error) {
	if f.Limit <= 0 {
		f.Limit = defaultPageLimit
	}
	if f.Limit > maxPageLimit {
		f.Limit = maxPageLimit
	}
	if f.Offset < 0 {
		f.Offset = 0
	}

	total, err := s.repo.Count(ctx, f)
	if err != nil {
		return nil, 0, err
	}
	events, err := s.repo.List(ctx, f)
	if err != nil {
		return nil, 0, err
	}
	return events, total, nil
}

// ValidationError บอกว่าผู้เรียกส่งอะไรมาผิด เพื่อให้ handler ตอบ 400 ได้
type ValidationError struct {
	Field  string
	Reason string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s %s", e.Field, e.Reason)
}
