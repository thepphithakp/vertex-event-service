package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	"github.com/vertex/event-service/internal/domain"
	"github.com/vertex/event-service/internal/port"
)

type fakeRepo struct {
	saved   []domain.EventLog
	seen    map[string]bool
	listErr error
}

func newFakeRepo() *fakeRepo { return &fakeRepo{seen: map[string]bool{}} }

func (f *fakeRepo) Save(_ context.Context, e *domain.EventLog) (bool, error) {
	if e.IdempotencyKey != nil {
		if f.seen[*e.IdempotencyKey] {
			return false, nil
		}
		f.seen[*e.IdempotencyKey] = true
	}
	f.saved = append(f.saved, *e)
	return true, nil
}

func (f *fakeRepo) List(context.Context, port.ListFilter) ([]domain.EventLog, error) {
	return f.saved, f.listErr
}

func (f *fakeRepo) Count(context.Context, port.ListFilter) (int64, error) {
	return int64(len(f.saved)), f.listErr
}

func ptr(s string) *string { return &s }

func TestCreateEvent_FillsMissingFields(t *testing.T) {
	repo := newFakeRepo()
	svc := NewEventService(repo)

	e := &domain.EventLog{EventType: "WaterLog", Action: "Water Intake Logged"}
	created, err := svc.CreateEvent(context.Background(), e)
	if err != nil {
		t.Fatalf("ไม่ควร error: %v", err)
	}
	if !created {
		t.Error("ต้องสร้างใหม่")
	}
	if e.ID == uuid.Nil {
		t.Error("ต้องเติม ID ให้")
	}
	if e.Timestamp.IsZero() {
		t.Error("ต้องเติม Timestamp ให้")
	}
}

func TestCreateEvent_Rejects(t *testing.T) {
	long := strings.Repeat("ก", maxFieldLength+1)

	cases := []struct {
		name  string
		event domain.EventLog
		field string
	}{
		{"ไม่มี eventType", domain.EventLog{Action: "x"}, "eventType"},
		{"eventType มีแต่ช่องว่าง", domain.EventLog{EventType: "   ", Action: "x"}, "eventType"},
		{"ไม่มี action", domain.EventLog{EventType: "x"}, "action"},
		{"eventType ยาวเกิน", domain.EventLog{EventType: long, Action: "x"}, "eventType"},
		{"entityId ยาวเกิน", domain.EventLog{EventType: "x", Action: "x", EntityID: long}, "entityId"},
		{
			"timestamp อยู่ในอนาคตไกลเกินไป",
			domain.EventLog{EventType: "x", Action: "x", Timestamp: time.Now().Add(24 * time.Hour)},
			"timestamp",
		},
		{
			"payload ใหญ่เกิน",
			domain.EventLog{EventType: "x", Action: "x",
				Payload: datatypes.JSON(`{"a":"` + strings.Repeat("x", maxPayloadBytes) + `"}`)},
			"payload",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := newFakeRepo()
			svc := NewEventService(repo)
			e := tc.event
			_, err := svc.CreateEvent(context.Background(), &e)

			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("ต้องได้ ValidationError แต่ได้ %v", err)
			}
			if ve.Field != tc.field {
				t.Errorf("field = %q ต้องเป็น %q", ve.Field, tc.field)
			}
			if len(repo.saved) != 0 {
				t.Error("ห้ามเขียนลงฐานข้อมูลเมื่อ validate ไม่ผ่าน")
			}
		})
	}
}

// TestCreateEvent_Idempotent ส่งซ้ำด้วยคีย์เดิมต้องไม่เกิดแถวใหม่
func TestCreateEvent_Idempotent(t *testing.T) {
	repo := newFakeRepo()
	svc := NewEventService(repo)

	mk := func() *domain.EventLog {
		return &domain.EventLog{
			EventType:      "WaterLog",
			Action:         "Water Intake Logged",
			IdempotencyKey: ptr("pet-1:water:2026-08-23T10:00:00Z"),
		}
	}

	if created, err := svc.CreateEvent(context.Background(), mk()); err != nil || !created {
		t.Fatalf("ครั้งแรกต้องสร้างได้: created=%v err=%v", created, err)
	}
	if created, err := svc.CreateEvent(context.Background(), mk()); err != nil || created {
		t.Fatalf("ครั้งที่สองต้องไม่สร้างใหม่: created=%v err=%v", created, err)
	}
	if len(repo.saved) != 1 {
		t.Errorf("มี %d แถว ต้องมีแถวเดียว", len(repo.saved))
	}
}

// TestCreateEvent_EmptyIdempotencyKeyBecomesNull
//
// ถ้าปล่อยค่าว่างผ่านไป แถวที่สองที่ส่งค่าว่างจะชน unique index
// แล้วกลายเป็นว่า event คนละตัวถูกมองว่าซ้ำกัน
func TestCreateEvent_EmptyIdempotencyKeyBecomesNull(t *testing.T) {
	repo := newFakeRepo()
	svc := NewEventService(repo)

	for i := 0; i < 2; i++ {
		e := &domain.EventLog{EventType: "x", Action: "y", IdempotencyKey: ptr("  ")}
		created, err := svc.CreateEvent(context.Background(), e)
		if err != nil || !created {
			t.Fatalf("ครั้งที่ %d ต้องสร้างได้: created=%v err=%v", i+1, created, err)
		}
		if e.IdempotencyKey != nil {
			t.Errorf("คีย์ที่มีแต่ช่องว่างต้องกลายเป็น nil ไม่ใช่ %q", *e.IdempotencyKey)
		}
	}
	if len(repo.saved) != 2 {
		t.Errorf("มี %d แถว ต้องมี 2 แถว", len(repo.saved))
	}
}

// TestListEvents_AlwaysBounded กันไม่ให้ดึงทั้งตาราง
func TestListEvents_AlwaysBounded(t *testing.T) {
	var got port.ListFilter
	repo := &capturingRepo{onList: func(f port.ListFilter) { got = f }}
	svc := NewEventService(repo)

	cases := []struct {
		name  string
		limit int
		want  int
	}{
		{"ไม่ระบุ limit → ใช้ค่า default", 0, defaultPageLimit},
		{"limit ติดลบ → ใช้ค่า default", -5, defaultPageLimit},
		{"limit เกินเพดาน → ถูกตัด", maxPageLimit + 1000, maxPageLimit},
		{"limit ปกติ → ใช้ตามที่ขอ", 10, 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := svc.ListEvents(context.Background(), port.ListFilter{Limit: tc.limit}); err != nil {
				t.Fatalf("ไม่ควร error: %v", err)
			}
			if got.Limit != tc.want {
				t.Errorf("limit = %d ต้องเป็น %d", got.Limit, tc.want)
			}
		})
	}
}

type capturingRepo struct{ onList func(port.ListFilter) }

func (c *capturingRepo) Save(context.Context, *domain.EventLog) (bool, error) { return true, nil }
func (c *capturingRepo) List(_ context.Context, f port.ListFilter) ([]domain.EventLog, error) {
	c.onList(f)
	return nil, nil
}
func (c *capturingRepo) Count(_ context.Context, f port.ListFilter) (int64, error) {
	c.onList(f)
	return 0, nil
}
