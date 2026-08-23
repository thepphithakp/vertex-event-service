package middleware

import (
	"log/slog"
	"os"
	"time"

	"github.com/gofiber/fiber/v2"
)

// infraPaths คือ endpoint ที่ถูกเรียกโดย k8s ไม่ใช่ผู้ใช้
//
// ไม่ log เพราะ probe ยิงทุกไม่กี่วินาทีตลอดเวลา ถ้า log ด้วยจะกลบ
// access log ของ request จริงจนหาไม่เจอ
var infraPaths = map[string]bool{
	"/livez":  true,
	"/readyz": true,
	"/health": true,
}

// IsInfraPath บอกว่า path นี้เป็นของ infrastructure ไม่ใช่ traffic ของผู้ใช้
func IsInfraPath(path string) bool { return infraPaths[path] }

// SetupLogger ตั้ง global logger เป็น JSON
//
// slog.SetDefault จะพา log ของ stdlib มาออกทาง handler นี้ด้วย
// ทำให้ทุกบรรทัดใน log parse เป็น JSON ได้เหมือนกันหมด
func SetupLogger(level string) {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})))
}

// NewAccessLog เขียน access log หนึ่งบรรทัดต่อหนึ่ง request
//
// ⚠️ จงใจไม่ log body — payload ของ event เป็นข้อมูลของผู้ใช้
// ถ้า log ด้วยจะกลายเป็นสำเนาข้อมูลส่วนตัวชุดที่สองในระบบ log
func NewAccessLog() fiber.Handler {
	return func(c *fiber.Ctx) error {
		if IsInfraPath(c.Path()) {
			return c.Next()
		}

		start := time.Now()
		err := c.Next()

		status := c.Response().StatusCode()
		attrs := []any{
			slog.String("method", c.Method()),
			slog.String("path", c.Path()),
			slog.Int("status", status),
			slog.Duration("latency", time.Since(start)),
			slog.String("request_id", c.Get(HeaderRequestID)),
			slog.String("ip", c.IP()),
		}
		if uid, ok := c.Locals("userId").(string); ok && uid != "" {
			attrs = append(attrs, slog.String("user_id", uid))
		}

		if status >= 500 {
			slog.ErrorContext(c.UserContext(), "http_request", attrs...)
		} else {
			slog.InfoContext(c.UserContext(), "http_request", attrs...)
		}
		return err
	}
}
