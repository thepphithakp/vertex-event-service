package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"

	"github.com/vertex/event-service/internal/bootstrap"
	"github.com/vertex/event-service/internal/config"
	"github.com/vertex/event-service/pkg/middleware"
)

// fatal จบ process พร้อม log ที่เป็น JSON เหมือน log line อื่น
//
// log.Fatal ของ stdlib ผ่าน bridge ของ slog ก็จริง แต่ออกมาเป็น level INFO เสมอ
// ทำให้ตอนไล่ปัญหา กรอง level=ERROR แล้วไม่เจอสาเหตุที่ทำให้ pod ตาย
func fatal(msg string, args ...any) {
	slog.Error(msg, args...)
	os.Exit(1)
}

func main() {
	// ตั้ง JSON logger ก่อนอ่าน config เพื่อให้ error ตอนอ่าน config เป็น JSON ด้วย
	middleware.SetupLogger("")

	cfg, err := config.Load()
	if err != nil {
		fatal("ตั้งค่าไม่ถูกต้อง", "error", err)
	}
	middleware.SetupLogger(cfg.Log.Level)

	keys, err := middleware.ParsePublicKeys(cfg.JWT.PublicKeys)
	if err != nil {
		fatal("อ่าน JWT_PUBLIC_KEYS ไม่สำเร็จ", "error", err)
	}
	for _, k := range keys {
		slog.Info("ยอมรับ public key", "kid", middleware.KeyID(k))
	}
	if len(keys) > 1 {
		slog.Warn("ตั้งค่า public key ไว้หลายใบ — โหมดนี้ใช้ระหว่าง rotate key เท่านั้น "+
			"เอาใบเก่าออกเมื่อผ่านไปนานกว่าอายุ token ที่ยาวที่สุด", "count", len(keys))
	}

	db, err := bootstrap.NewDB(cfg.DB)
	if err != nil {
		fatal("เชื่อมต่อฐานข้อมูลไม่สำเร็จ", "error", err)
	}

	// schema จัดการโดย Flyway แล้ว ไม่ใช่ AutoMigrate
	if err := bootstrap.AssertSchemaVersion(context.Background(), db); err != nil {
		fatal("schema ยังไม่พร้อม (Flyway migration รันครบหรือยัง)", "error", err)
	}

	app, health := bootstrap.NewApp(db, cfg, middleware.AuthConfig{
		PublicKeys: keys,
		Issuer:     cfg.JWT.Issuer,
		Audience:   cfg.JWT.Audience,
	})

	go func() {
		slog.Info("event-service กำลังรับ request", "port", cfg.Port)
		if err := app.Listen(":" + cfg.Port); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("listen ล้มเหลว", "error", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	shutdown(app, health, db, cfg.Shutdown)
}

// shutdown ปิดตัวโดยไม่ทิ้ง request ที่ค้างอยู่
//
// ลำดับสำคัญ: ปิด readiness ก่อน แล้วรอให้ kube-proxy ทุก node อัปเดต
// iptables ตาม จากนั้นค่อยหยุดรับ connection ใหม่
// ถ้าปิด listener เลย request ที่ k8s เพิ่งส่งมาจะถูกตัดกลางทาง
func shutdown(app *fiber.App, health *bootstrap.Health, db *gorm.DB, cfg config.ShutdownConfig) {
	slog.Info("ได้รับสัญญาณปิด เริ่มปิดตัวแบบ graceful")

	health.BeginShutdown()
	slog.Info("ปิด readiness แล้ว รอให้ k8s ถอด endpoint", "wait", cfg.DrainDelay)
	time.Sleep(cfg.DrainDelay)

	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()

	if err := app.ShutdownWithContext(ctx); err != nil {
		slog.Error("ปิด HTTP server ไม่เรียบร้อย", "error", err)
	} else {
		slog.Info("request ที่ค้างอยู่ทำงานจนจบแล้ว")
	}

	if err := bootstrap.CloseDB(db); err != nil {
		slog.Error("ปิด connection ฐานข้อมูลไม่เรียบร้อย", "error", err)
	}
	slog.Info("ปิดตัวเรียบร้อย")
}
