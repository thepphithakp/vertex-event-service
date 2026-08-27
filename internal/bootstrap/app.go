package bootstrap

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"gorm.io/gorm"

	"github.com/vertex/event-service/internal/adapter/handler"
	"github.com/vertex/event-service/internal/adapter/repository"
	"github.com/vertex/event-service/internal/application"
	"github.com/vertex/event-service/internal/config"
	"github.com/vertex/event-service/pkg/middleware"
)

// bodyLimit จำกัดขนาด request
//
// event หนึ่งตัวไม่ควรใหญ่ — payload ถูกจำกัดที่ 16KB อยู่แล้วในชั้น service
// ตั้งเพดานตรงนี้อีกชั้นเพื่อไม่ให้ต้องอ่าน body ทั้งก้อนเข้าหน่วยความจำก่อน
const bodyLimit = 256 << 10 // 256KB

// NewApp ประกอบ HTTP layer ทั้งหมด
func NewApp(db *gorm.DB, cfg config.Config, auth middleware.AuthConfig) (*fiber.App, *Health) {
	app := fiber.New(fiber.Config{
		BodyLimit:    bodyLimit,
		ErrorHandler: middleware.ErrorHandler,
		// กล่อง ASCII ตอน start เป็นบรรทัดเดียวใน log ที่ parse เป็น JSON ไม่ได้
		DisableStartupMessage: true,
	})

	// recover ต้องมาก่อนทุกอย่าง — panic ใน handler ไม่ควรทำให้ทั้ง pod ตาย
	app.Use(recover.New())
	app.Use(middleware.NewRequestID())
	// metrics มาก่อน access log เพื่อให้นับ request ที่ถูกปฏิเสธตั้งแต่ต้นทางด้วย
	app.Use(middleware.NewMetrics())
	app.Use(middleware.NewAccessLog())
	app.Use(cors.New())

	health := NewHealth(db)
	app.Get("/livez", health.Liveness)
	app.Get("/readyz", health.Readiness)
	// คงไว้เพื่อความเข้ากันได้กับ monitoring เดิม
	app.Get("/health", health.Liveness)
	// ให้ Prometheus มาดึง — เข้าถึงได้จากในคลัสเตอร์เท่านั้น
	// เพราะ ingress route เฉพาะ prefix /api/v1 เข้ามา
	app.Get("/metrics", middleware.MetricsHandler())

	repo := repository.NewGORMEventRepository(db)
	svc := application.NewEventService(repo)
	h := handler.NewEventHandler(svc)

	// ── ingest: service อื่นเรียก ต้องมี service token ────────────────────────
	//
	// ⚠️ ห้ามใช้ app.Group("/api/v1", serviceToken) ตรงนี้
	//    Fiber ผูก middleware ของ group ตาม prefix ซึ่ง "/api/v1" ครอบ
	//    "/api/v1/admin/..." ไปด้วย ผลคือ backoffice ที่ส่ง JWT มาถูกปฏิเสธ
	//    ด้วยข้อความว่า service token ผิด (เทสต์ integration จับได้)
	h.RegisterIngestRoutes(app.Group("/api/v1/events",
		middleware.NewServiceToken(cfg.Ingest.Token)))

	// ── admin: backoffice เรียก ต้องเป็น SUPER_ADMIN ─────────────────────────
	//
	// 🔴 ก่อนหน้านี้ route นี้ไม่มีการตรวจอะไรเลย ใครก็ตามบนอินเทอร์เน็ต
	//    ดึง event log ทั้งหมดได้ ซึ่งมี user id และ pet id ของผู้ใช้จริง
	admin := app.Group("/api/v1/admin",
		middleware.NewAuth(auth),
		middleware.RequireRole(middleware.RoleSuperAdmin),
	)
	h.RegisterAdminRoutes(admin)

	return app, health
}
