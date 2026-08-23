package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

// TestServiceToken กันไม่ให้ POST /events กลับไปเปิดโล่งอีก
//
// ก่อนหน้านี้ใครก็ยิง event ปลอมเข้าระบบได้ — เขียนอะไรลง audit log
// ก็ได้ในนามของ user คนไหนก็ได้ ซึ่งทำให้ log ที่มีไว้ตรวจสอบเชื่อถือไม่ได้
func TestServiceToken(t *testing.T) {
	const secret = "a-very-long-service-token-value-1234567890"

	app := fiber.New()
	app.Use(NewRequestID())
	g := app.Group("/api/v1", NewServiceToken(secret))
	g.Post("/events", func(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusCreated) })

	cases := []struct {
		name  string
		token string
		want  int
	}{
		{"token ถูกต้อง", secret, fiber.StatusCreated},
		{"ไม่ส่ง token", "", fiber.StatusUnauthorized},
		{"token ผิด", "wrong-token", fiber.StatusUnauthorized},
		{"token ถูกแต่ขาดตัวท้าย", secret[:len(secret)-1], fiber.StatusUnauthorized},
		{"token ถูกแต่มีตัวเกิน", secret + "x", fiber.StatusUnauthorized},
		{"ตัวพิมพ์ต่างกัน", "A-VERY-LONG-SERVICE-TOKEN-VALUE-1234567890", fiber.StatusUnauthorized},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/v1/events", nil)
			if tc.token != "" {
				req.Header.Set(HeaderServiceToken, tc.token)
			}
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("ยิง request ไม่สำเร็จ: %v", err)
			}
			if resp.StatusCode != tc.want {
				t.Errorf("status = %d ต้องเป็น %d", resp.StatusCode, tc.want)
			}
		})
	}
}

// TestServiceToken_JWTไม่ใช่ทางเข้า
//
// ผู้ใช้ที่ login แล้วไม่ควรยิง event เข้าระบบเองได้
// เส้นทางนี้ต้องเปิดให้เฉพาะ service ที่ถือ token เท่านั้น
func TestServiceToken_UserJWTIsNotAccepted(t *testing.T) {
	const secret = "a-very-long-service-token-value-1234567890"

	app := fiber.New()
	app.Use(NewRequestID())
	g := app.Group("/api/v1", NewServiceToken(secret))
	g.Post("/events", func(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusCreated) })

	req := httptest.NewRequest("POST", "/api/v1/events", nil)
	req.Header.Set("Authorization", "Bearer some.jwt.token")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("ยิง request ไม่สำเร็จ: %v", err)
	}
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("status = %d ต้องเป็น 401 — Authorization header ไม่ใช่ทางเข้าของ endpoint นี้", resp.StatusCode)
	}
}
