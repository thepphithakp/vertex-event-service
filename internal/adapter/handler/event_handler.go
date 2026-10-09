package handler

import (
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"

	"github.com/vertex/event-service/internal/application"
	"github.com/vertex/event-service/internal/domain"
	"github.com/vertex/event-service/internal/port"
	"github.com/vertex/event-service/pkg/middleware"
)

// createEventRequest คือรูปแบบ JSON ที่รับจากผู้เรียก — เดิม BodyParser parse
// ลง domain.EventLog ตรงๆ ย้าย wire format มาที่นี่ไม่ให้ domain ต้องรู้จัก
// JSON เลย field/ชื่อเหมือนเดิมทุกตัวอักษร ไม่ให้ caller ที่ยิง event เข้ามา
// (chat-service, pet-service ฯลฯ) พัง
type createEventRequest struct {
	ID             uuid.UUID       `json:"id"`
	Timestamp      time.Time       `json:"timestamp"`
	EventType      string          `json:"eventType"`
	Action         string          `json:"action"`
	ActorID        string          `json:"actorId"`
	ActorUsername  string          `json:"actorUsername"`
	EntityID       string          `json:"entityId"`
	EntityType     string          `json:"entityType"`
	Payload        json.RawMessage `json:"payload"`
	IdempotencyKey *string         `json:"idempotencyKey,omitempty"`
}

func (r createEventRequest) toDomain() domain.EventLog {
	return domain.EventLog{
		ID:             r.ID,
		Timestamp:      r.Timestamp,
		EventType:      r.EventType,
		Action:         r.Action,
		ActorID:        r.ActorID,
		ActorUsername:  r.ActorUsername,
		EntityID:       r.EntityID,
		EntityType:     r.EntityType,
		Payload:        r.Payload,
		IdempotencyKey: r.IdempotencyKey,
	}
}

// eventLogResponse คือรูปแบบ JSON ที่ตอบกลับ — รูปแบบเดิมทุกตัวอักษร (เดิม
// domain.EventLog มี json tag ติดอยู่กับตัวเอง)
type eventLogResponse struct {
	ID             uuid.UUID       `json:"id"`
	Timestamp      time.Time       `json:"timestamp"`
	CreatedAt      time.Time       `json:"createdAt"`
	EventType      string          `json:"eventType"`
	Action         string          `json:"action"`
	ActorID        string          `json:"actorId"`
	ActorUsername  string          `json:"actorUsername"`
	EntityID       string          `json:"entityId"`
	EntityType     string          `json:"entityType"`
	Payload        json.RawMessage `json:"payload"`
	IdempotencyKey *string         `json:"idempotencyKey,omitempty"`
}

func toEventLogResponse(e domain.EventLog) eventLogResponse {
	return eventLogResponse{
		ID:             e.ID,
		Timestamp:      e.Timestamp,
		CreatedAt:      e.CreatedAt,
		EventType:      e.EventType,
		Action:         e.Action,
		ActorID:        e.ActorID,
		ActorUsername:  e.ActorUsername,
		EntityID:       e.EntityID,
		EntityType:     e.EntityType,
		Payload:        e.Payload,
		IdempotencyKey: e.IdempotencyKey,
	}
}

type EventHandler struct {
	useCase port.EventUseCase
}

func NewEventHandler(useCase port.EventUseCase) *EventHandler {
	return &EventHandler{useCase: useCase}
}

// RegisterIngestRoutes ผูก endpoint ที่ service อื่นเรียก
//
// แยกจาก admin route เพราะใช้วิธียืนยันตัวตนคนละแบบ
// (service token ไม่ใช่ JWT ของผู้ใช้) การแยกทำให้เห็นชัดว่า
// endpoint ไหนถูกป้องกันด้วยอะไร
func (h *EventHandler) RegisterIngestRoutes(r fiber.Router) {
	// path ว่างเพราะ prefix ของ group คือ /api/v1/events อยู่แล้ว
	r.Post("", h.CreateEvent)
	r.Post("/", h.CreateEvent)
}

// RegisterAdminRoutes ผูก endpoint ที่ backoffice เรียก
func (h *EventHandler) RegisterAdminRoutes(r fiber.Router) {
	r.Get("/events", h.ListEvents)
}

func (h *EventHandler) CreateEvent(c *fiber.Ctx) error {
	var req createEventRequest
	if err := c.BodyParser(&req); err != nil {
		return badRequest(c, "อ่าน request body ไม่ได้")
	}
	event := req.toDomain()

	created, err := h.useCase.CreateEvent(c.UserContext(), &event)
	if err != nil {
		var ve *application.ValidationError
		if errors.As(err, &ve) {
			return badRequest(c, ve.Error())
		}
		return internalError(c, err)
	}

	// ส่งซ้ำด้วย idempotency key เดิมไม่ใช่ error — ตอบ 200 เพื่อบอกว่า
	// "รับทราบแล้ว แต่ไม่ได้สร้างใหม่" ต่างจาก 201 ที่แปลว่าสร้างจริง
	if !created {
		return c.Status(fiber.StatusOK).JSON(fiber.Map{
			"id":        event.ID,
			"duplicate": true,
		})
	}
	return c.Status(fiber.StatusCreated).JSON(toEventLogResponse(event))
}

func (h *EventHandler) ListEvents(c *fiber.Ctx) error {
	f := port.ListFilter{
		EntityType: c.Query("entityType"),
		EntityID:   c.Query("entityId"),
		ActorID:    c.Query("actorId"),
		Limit:      queryInt(c, "limit", 0),
		Offset:     queryInt(c, "offset", 0),
	}

	events, total, err := h.useCase.ListEvents(c.UserContext(), f)
	if err != nil {
		return internalError(c, err)
	}

	items := make([]eventLogResponse, len(events))
	for i, e := range events {
		items[i] = toEventLogResponse(e)
	}
	return c.JSON(fiber.Map{
		"data":   items,
		"total":  total,
		"limit":  len(events),
		"offset": f.Offset,
	})
}

func queryInt(c *fiber.Ctx, key string, fallback int) int {
	v := c.Query(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func badRequest(c *fiber.Ctx, msg string) error {
	return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
		"error":     msg,
		"requestId": c.Get(middleware.HeaderRequestID),
	})
}

// internalError ไม่ส่งรายละเอียดของ error กลับไปให้ผู้เรียก
//
// ข้อความจาก database มักมีชื่อตาราง ชื่อคอลัมน์ และบางครั้งมีค่าที่ทำให้ล้ม
// ซึ่งเป็นข้อมูลที่ช่วยคนที่พยายามโจมตี — เก็บไว้ใน log ฝั่งเราแทน
func internalError(c *fiber.Ctx, err error) error {
	return fiber.NewError(fiber.StatusInternalServerError, err.Error())
}
