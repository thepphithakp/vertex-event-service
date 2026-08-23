package bootstrap

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

const dbPingTimeout = 2 * time.Second

// Health แยก liveness ออกจาก readiness
type Health struct {
	db           *gorm.DB
	shuttingDown atomic.Bool
}

func NewHealth(db *gorm.DB) *Health { return &Health{db: db} }

// BeginShutdown ทำให้ readiness ตอบไม่พร้อมทันที
// เรียกตอนได้รับ SIGTERM เพื่อให้ k8s ถอด pod ออกจาก endpoints ก่อนปิดจริง
func (h *Health) BeginShutdown() { h.shuttingDown.Store(true) }

// Liveness ตอบว่า process ยังอยู่ — จงใจไม่เช็ค dependency ใดๆ
//
// ถ้าเช็ค DB ด้วย พอ DB ล่ม k8s จะฆ่าทุก pod พร้อมกันแล้ววนรีสตาร์ตไม่จบ
// ทำให้เหตุการณ์แย่ลงแทนที่จะดีขึ้น
func (h *Health) Liveness(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"status": "ok"})
}

// Readiness เช็คว่าพร้อมรับ traffic ไหม
//
// ล้มแล้ว pod แค่ถูกถอดออกจาก endpoints ไม่ถูกฆ่า — พอ DB กลับมาก็รับ traffic ต่อ
func (h *Health) Readiness(c *fiber.Ctx) error {
	if h.shuttingDown.Load() {
		return c.Status(fiber.StatusServiceUnavailable).
			JSON(fiber.Map{"status": "shutting_down"})
	}
	if h.db == nil {
		return c.Status(fiber.StatusServiceUnavailable).
			JSON(fiber.Map{"status": "no_database"})
	}

	sqlDB, err := h.db.DB()
	if err != nil {
		return c.Status(fiber.StatusServiceUnavailable).
			JSON(fiber.Map{"status": "database_unavailable"})
	}

	ctx, cancel := context.WithTimeout(c.UserContext(), dbPingTimeout)
	defer cancel()
	if err := sqlDB.PingContext(ctx); err != nil {
		return c.Status(fiber.StatusServiceUnavailable).
			JSON(fiber.Map{"status": "database_unreachable"})
	}

	return c.JSON(fiber.Map{"status": "ready"})
}
