//go:build integration

package bootstrap

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/vertex/event-service/internal/config"
	"github.com/vertex/event-service/pkg/middleware"
)

const testIngestToken = "integration-test-service-token-1234567890"

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("ไม่ได้ตั้ง TEST_DATABASE_URL — ข้าม integration test")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("ต่อฐานข้อมูลไม่ได้: %v", err)
	}
	assertDisposableDB(t, db)
	return db
}

// assertDisposableDB กันไม่ให้ integration test ไปรันใส่ฐานข้อมูลจริง
//
// เทสต์ชุดนี้เขียนและลบข้อมูล ถ้ามี port-forward ของ postgres ใน cluster
// ค้างอยู่ที่พอร์ตเดียวกัน DSN อาจชี้ไป production โดยไม่ตั้งใจ
func assertDisposableDB(t *testing.T, db *gorm.DB) {
	t.Helper()

	var dbName string
	if err := db.Raw("SELECT current_database()").Scan(&dbName).Error; err != nil {
		t.Fatalf("อ่านชื่อฐานข้อมูลไม่ได้: %v", err)
	}
	// ฐานข้อมูลสำหรับเทสต์ต้องมีคำว่า test อยู่ในชื่อเสมอ
	if !bytes.Contains([]byte(dbName), []byte("test")) {
		t.Fatalf("ปฏิเสธการรัน: ฐานข้อมูล %q ไม่มีคำว่า \"test\" ในชื่อ "+
			"จึงถือว่าไม่ใช่ฐานข้อมูลชั่วคราวที่ทิ้งได้", dbName)
	}
}

func newTestApp(t *testing.T, db *gorm.DB) (*fiber.App, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("สร้างคีย์ไม่สำเร็จ: %v", err)
	}
	cfg := config.Config{
		Port:   "0",
		Ingest: config.IngestConfig{Token: testIngestToken},
	}
	app, _ := NewApp(db, cfg, middleware.AuthConfig{
		PublicKeys: []*rsa.PublicKey{&key.PublicKey},
	})
	return app, key
}

func adminToken(t *testing.T, key *rsa.PrivateKey, roles ...string) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"sub":   "af8ebb2e-1b91-4b12-9aa8-5424a2eb09b9",
		"roles": roles,
		"exp":   time.Now().Add(time.Hour).Unix(),
	})
	tok.Header["kid"] = middleware.KeyID(&key.PublicKey)
	s, err := tok.SignedString(key)
	if err != nil {
		t.Fatalf("เซ็น token ไม่สำเร็จ: %v", err)
	}
	return s
}

func do(t *testing.T, app *fiber.App, req *http.Request) (int, []byte) {
	t.Helper()
	resp, err := app.Test(req, 10_000)
	if err != nil {
		t.Fatalf("ยิง request ไม่สำเร็จ: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

func postEvent(t *testing.T, app *fiber.App, token string, payload map[string]any) (int, []byte) {
	t.Helper()
	b, _ := json.Marshal(payload)
	req := httptest.NewRequest("POST", "/api/v1/events", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set(middleware.HeaderServiceToken, token)
	}
	return do(t, app, req)
}

// TestEventLog_NotReadableWithoutSuperAdmin คือเทสต์ที่สำคัญที่สุดของ Phase 10
//
// ก่อนหน้านี้ GET /api/v1/admin/events เปิดโล่ง ใครก็ตามบนอินเทอร์เน็ต
// ดึง event log ทั้งหมดได้ ซึ่งมี user id และ pet id ของผู้ใช้จริง
func TestEventLog_NotReadableWithoutSuperAdmin(t *testing.T) {
	db := openTestDB(t)
	app, key := newTestApp(t, db)

	cases := []struct {
		name  string
		authz string
		want  int
	}{
		{"ไม่มี token เลย", "", fiber.StatusUnauthorized},
		{"token มั่ว", "Bearer abc", fiber.StatusUnauthorized},
		{"USER ธรรมดา", "Bearer " + adminToken(t, key, middleware.RoleUser), fiber.StatusForbidden},
		{"SUPER_ADMIN", "Bearer " + adminToken(t, key, middleware.RoleSuperAdmin), fiber.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/v1/admin/events", nil)
			if tc.authz != "" {
				req.Header.Set("Authorization", tc.authz)
			}
			got, body := do(t, app, req)
			if got != tc.want {
				t.Fatalf("status = %d ต้องเป็น %d (body: %s)", got, tc.want, body)
			}
			if tc.want != fiber.StatusOK && bytes.Contains(body, []byte("eventType")) {
				t.Error("ข้อมูล event รั่วออกไปทั้งที่ไม่มีสิทธิ์")
			}
		})
	}
}

// TestIngest_RequiresServiceToken กันไม่ให้ใครยิง event ปลอมเข้าระบบ
func TestIngest_RequiresServiceToken(t *testing.T) {
	db := openTestDB(t)
	app, key := newTestApp(t, db)

	ev := map[string]any{"eventType": "test", "action": "no-token"}

	if got, _ := postEvent(t, app, "", ev); got != fiber.StatusUnauthorized {
		t.Errorf("ไม่มี service token: status = %d ต้องเป็น 401", got)
	}
	if got, _ := postEvent(t, app, "wrong", ev); got != fiber.StatusUnauthorized {
		t.Errorf("service token ผิด: status = %d ต้องเป็น 401", got)
	}

	// JWT ของผู้ใช้ก็ไม่ใช่ทางเข้าของ endpoint นี้
	b, _ := json.Marshal(ev)
	req := httptest.NewRequest("POST", "/api/v1/events", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken(t, key, middleware.RoleSuperAdmin))
	if got, _ := do(t, app, req); got != fiber.StatusUnauthorized {
		t.Errorf("JWT ของ SUPER_ADMIN: status = %d ต้องเป็น 401", got)
	}
}

// TestIngest_WritesAndIsIdempotent พิสูจน์ทางเดินข้อมูลจริงตั้งแต่ HTTP ถึง database
func TestIngest_WritesAndIsIdempotent(t *testing.T) {
	db := openTestDB(t)
	app, key := newTestApp(t, db)

	entityID := fmt.Sprintf("pet-%d", time.Now().UnixNano())
	idemKey := entityID + ":water:1"
	t.Cleanup(func() {
		db.Exec("DELETE FROM event_logs WHERE entity_id = ?", entityID)
	})

	ev := map[string]any{
		"eventType":      "WaterLog",
		"action":         "Water Intake Logged",
		"entityType":     "Pet",
		"entityId":       entityID,
		"actorId":        "af8ebb2e-1b91-4b12-9aa8-5424a2eb09b9",
		"payload":        map[string]any{"amount": 20},
		"idempotencyKey": idemKey,
	}

	if got, body := postEvent(t, app, testIngestToken, ev); got != fiber.StatusCreated {
		t.Fatalf("ครั้งแรก status = %d ต้องเป็น 201 (body: %s)", got, body)
	}

	// ส่งซ้ำด้วยคีย์เดิม — ต้องไม่เกิดแถวใหม่
	got, body := postEvent(t, app, testIngestToken, ev)
	if got != fiber.StatusOK {
		t.Fatalf("ส่งซ้ำ status = %d ต้องเป็น 200 (body: %s)", got, body)
	}
	if !bytes.Contains(body, []byte(`"duplicate":true`)) {
		t.Errorf("ส่งซ้ำต้องบอกว่า duplicate: %s", body)
	}

	var n int64
	db.Raw("SELECT count(*) FROM event_logs WHERE entity_id = ?", entityID).Scan(&n)
	if n != 1 {
		t.Fatalf("มี %d แถว ต้องมีแถวเดียว — idempotency ไม่ทำงาน", n)
	}

	// created_at ต้องถูกใส่โดย database ไม่ใช่ผู้เรียก
	var createdAt time.Time
	db.Raw("SELECT created_at FROM event_logs WHERE entity_id = ?", entityID).Scan(&createdAt)
	if createdAt.IsZero() {
		t.Error("created_at ต้องมีค่าเสมอ")
	}

	// อ่านกลับผ่าน admin API ได้
	req := httptest.NewRequest("GET", "/api/v1/admin/events?entityId="+entityID, nil)
	req.Header.Set("Authorization", "Bearer "+adminToken(t, key, middleware.RoleSuperAdmin))
	st, listBody := do(t, app, req)
	if st != fiber.StatusOK {
		t.Fatalf("อ่านกลับ status = %d ต้องเป็น 200", st)
	}
	if !bytes.Contains(listBody, []byte(entityID)) {
		t.Errorf("ไม่พบ event ที่เพิ่งเขียน: %s", listBody)
	}
}

// TestIngest_RejectsGarbage ข้อมูลที่ใช้ไม่ได้ต้องถูกปัดตกที่ 400 ไม่ใช่ 500
func TestIngest_RejectsGarbage(t *testing.T) {
	db := openTestDB(t)
	app, _ := newTestApp(t, db)

	cases := []struct {
		name  string
		event map[string]any
	}{
		{"ไม่มี eventType", map[string]any{"action": "x"}},
		{"ไม่มี action", map[string]any{"eventType": "x"}},
		{"timestamp อยู่ในอนาคต", map[string]any{
			"eventType": "x", "action": "y",
			"timestamp": time.Now().Add(48 * time.Hour).Format(time.RFC3339),
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, body := postEvent(t, app, testIngestToken, tc.event)
			if got != fiber.StatusBadRequest {
				t.Errorf("status = %d ต้องเป็น 400 (body: %s)", got, body)
			}
		})
	}
}

// TestList_IsAlwaysBounded ไม่มีทางดึงทั้งตารางออกมาได้
func TestList_IsAlwaysBounded(t *testing.T) {
	db := openTestDB(t)
	app, key := newTestApp(t, db)

	req := httptest.NewRequest("GET", "/api/v1/admin/events?limit=999999", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken(t, key, middleware.RoleSuperAdmin))
	st, body := do(t, app, req)
	if st != fiber.StatusOK {
		t.Fatalf("status = %d ต้องเป็น 200", st)
	}

	var resp struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("อ่าน response ไม่ได้: %v", err)
	}
	if len(resp.Data) > 500 {
		t.Errorf("คืนมา %d แถว ต้องไม่เกินเพดาน 500", len(resp.Data))
	}
}

// TestHealth_LivezIgnoresDatabase
func TestHealth_LivezIgnoresDatabase(t *testing.T) {
	db := openTestDB(t)
	app, _ := newTestApp(t, db)

	for _, path := range []string{"/livez", "/readyz"} {
		st, _ := do(t, app, httptest.NewRequest("GET", path, nil))
		if st != fiber.StatusOK {
			t.Errorf("%s status = %d ต้องเป็น 200", path, st)
		}
	}
}
