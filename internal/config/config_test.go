package config

import (
	"strings"
	"testing"
)

func setEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	for k, v := range kv {
		t.Setenv(k, v)
	}
}

func validEnv() map[string]string {
	return map[string]string{
		"DB_USER":            "event_app",
		"DB_PASSWORD":        "pw",
		"JWT_PUBLIC_KEYS":    "-----BEGIN PUBLIC KEY-----\nx\n-----END PUBLIC KEY-----",
		"EVENT_INGEST_TOKEN": strings.Repeat("t", 48),
	}
}

// TestLoad_RefusesToStartWithoutRequiredValues
//
// เดิม main.go มี DSN พร้อมรหัสผ่าน hardcode ไว้เป็น fallback
// ตั้งค่าผิดแล้วไม่พัง แต่ไปต่อ database ผิดตัวเงียบๆ
func TestLoad_RefusesToStartWithoutRequiredValues(t *testing.T) {
	for _, missing := range []string{"DB_USER", "DB_PASSWORD", "JWT_PUBLIC_KEYS", "EVENT_INGEST_TOKEN"} {
		t.Run("ขาด "+missing, func(t *testing.T) {
			env := validEnv()
			env[missing] = ""
			setEnv(t, env)

			_, err := Load()
			if err == nil {
				t.Fatalf("ต้อง error เมื่อไม่ได้ตั้ง %s", missing)
			}
			if !strings.Contains(err.Error(), missing) {
				t.Errorf("ข้อความ error ต้องบอกว่าขาดตัวไหน: %v", err)
			}
		})
	}
}

// TestLoad_RejectsWeakIngestToken
//
// token สั้นๆ อย่าง "secret" เดาได้ในไม่กี่วินาที ซึ่งเท่ากับไม่มี auth
func TestLoad_RejectsWeakIngestToken(t *testing.T) {
	env := validEnv()
	env["EVENT_INGEST_TOKEN"] = "secret"
	setEnv(t, env)

	if _, err := Load(); err == nil {
		t.Fatal("token สั้นเกินไปต้องไม่ผ่าน")
	}
}

func TestLoad_Defaults(t *testing.T) {
	setEnv(t, validEnv())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("ไม่ควร error: %v", err)
	}
	if cfg.Port != "4002" {
		t.Errorf("Port = %q ต้องเป็น 4002", cfg.Port)
	}
	if cfg.DB.SearchPath != "event" {
		t.Errorf("SearchPath = %q ต้องเป็น event", cfg.DB.SearchPath)
	}
	if cfg.Shutdown.DrainDelay.Seconds() != 5 {
		t.Errorf("DrainDelay = %v ต้องเป็น 5s", cfg.Shutdown.DrainDelay)
	}
}

// TestRedacted_HidesPassword — DSN ถูก log ตอนต่อ DB ไม่สำเร็จ
func TestRedacted_HidesPassword(t *testing.T) {
	c := DBConfig{Host: "h", Port: "5432", User: "u", Password: "ลับมาก", Name: "vertex", SearchPath: "event"}
	if strings.Contains(c.Redacted(), "ลับมาก") {
		t.Errorf("Redacted ต้องไม่มีรหัสผ่าน: %s", c.Redacted())
	}
	if !strings.Contains(c.DSN(), "ลับมาก") {
		t.Error("DSN ต้องมีรหัสผ่านจริง ไม่งั้นต่อไม่ได้")
	}
}
