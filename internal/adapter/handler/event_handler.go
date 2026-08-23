package handler

import (
	"errors"
	"strconv"

	"github.com/gofiber/fiber/v2"

	"github.com/vertex/event-service/internal/application"
	"github.com/vertex/event-service/internal/domain"
	"github.com/vertex/event-service/internal/port"
	"github.com/vertex/event-service/pkg/middleware"
)

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
	var event domain.EventLog
	if err := c.BodyParser(&event); err != nil {
		return badRequest(c, "อ่าน request body ไม่ได้")
	}

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
	return c.Status(fiber.StatusCreated).JSON(event)
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

	return c.JSON(fiber.Map{
		"data":   events,
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
