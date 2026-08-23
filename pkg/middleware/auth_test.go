package middleware

import (
	"crypto/rand"
	"crypto/rsa"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
)

func signToken(t *testing.T, key *rsa.PrivateKey, claims jwt.MapClaims, withKID bool) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	if withKID {
		tok.Header["kid"] = KeyID(&key.PublicKey)
	}
	s, err := tok.SignedString(key)
	if err != nil {
		t.Fatalf("เซ็น token ไม่สำเร็จ: %v", err)
	}
	return s
}

func protectedApp(t *testing.T, keys []*rsa.PublicKey, roles ...string) *fiber.App {
	t.Helper()
	app := fiber.New()
	app.Use(NewRequestID())
	g := app.Group("/admin", NewAuth(AuthConfig{PublicKeys: keys}), RequireRole(roles...))
	g.Get("/events", func(c *fiber.Ctx) error { return c.SendString("secret") })
	return app
}

func status(t *testing.T, app *fiber.App, path, authz string) int {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	if authz != "" {
		req.Header.Set("Authorization", authz)
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("ยิง request ไม่สำเร็จ: %v", err)
	}
	return resp.StatusCode
}

// TestAdminRoute_ClosedToAnonymous คือเทสต์ที่สำคัญที่สุดของไฟล์นี้
//
// ก่อนหน้านี้ GET /api/v1/admin/events ไม่มีการตรวจอะไรเลย
// ใครก็ตามบนอินเทอร์เน็ตดึง event log ทั้งหมดได้ ซึ่งมี user id
// และ pet id ของผู้ใช้จริง เทสต์นี้กันไม่ให้กลับไปเป็นแบบนั้นอีก
func TestAdminRoute_ClosedToAnonymous(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	app := protectedApp(t, []*rsa.PublicKey{&key.PublicKey}, RoleSuperAdmin)

	cases := []struct {
		name  string
		authz string
	}{
		{"ไม่ส่ง header เลย", ""},
		{"header ว่าง", "Bearer "},
		{"ไม่ใช่ bearer", "Basic YWRtaW46YWRtaW4="},
		{"token มั่ว", "Bearer not-a-token"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := status(t, app, "/admin/events", tc.authz); got != fiber.StatusUnauthorized {
				t.Errorf("status = %d ต้องเป็น 401", got)
			}
		})
	}
}

// TestAdminRoute_RequiresSuperAdmin ผู้ใช้ทั่วไปที่ login แล้วก็ยังเข้าไม่ได้
func TestAdminRoute_RequiresSuperAdmin(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	app := protectedApp(t, []*rsa.PublicKey{&key.PublicKey}, RoleSuperAdmin)

	base := func(roles []string) jwt.MapClaims {
		c := jwt.MapClaims{
			"sub": "af8ebb2e-1b91-4b12-9aa8-5424a2eb09b9",
			"exp": time.Now().Add(time.Hour).Unix(),
		}
		if roles != nil {
			c["roles"] = roles
		}
		return c
	}

	cases := []struct {
		name  string
		roles []string
		want  int
	}{
		{"SUPER_ADMIN เข้าได้", []string{RoleSuperAdmin}, fiber.StatusOK},
		{"SUPER_ADMIN ปนกับ role อื่น", []string{RoleUser, RoleSuperAdmin}, fiber.StatusOK},
		{"USER ธรรมดาถูกปฏิเสธ", []string{RoleUser}, fiber.StatusForbidden},
		{"PET_ADMIN ก็ยังไม่พอ", []string{RolePetAdmin}, fiber.StatusForbidden},
		{"token ที่ไม่มี roles ถือเป็น USER", nil, fiber.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tok := signToken(t, key, base(tc.roles), true)
			if got := status(t, app, "/admin/events", "Bearer "+tok); got != tc.want {
				t.Errorf("status = %d ต้องเป็น %d", got, tc.want)
			}
		})
	}
}

// TestAuth_RejectsForeignAndExpiredTokens
func TestAuth_RejectsForeignAndExpiredTokens(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	app := protectedApp(t, []*rsa.PublicKey{&key.PublicKey}, RoleSuperAdmin)

	claims := func(exp time.Time) jwt.MapClaims {
		return jwt.MapClaims{
			"sub":   "af8ebb2e-1b91-4b12-9aa8-5424a2eb09b9",
			"roles": []string{RoleSuperAdmin},
			"exp":   exp.Unix(),
		}
	}

	t.Run("เซ็นด้วยคีย์ที่ไม่ได้อยู่ในรายการ", func(t *testing.T) {
		tok := signToken(t, other, claims(time.Now().Add(time.Hour)), false)
		if got := status(t, app, "/admin/events", "Bearer "+tok); got != fiber.StatusUnauthorized {
			t.Errorf("status = %d ต้องเป็น 401 — คีย์ปลอมต้องผ่านไม่ได้", got)
		}
	})

	t.Run("token หมดอายุแล้ว", func(t *testing.T) {
		tok := signToken(t, key, claims(time.Now().Add(-time.Hour)), true)
		if got := status(t, app, "/admin/events", "Bearer "+tok); got != fiber.StatusUnauthorized {
			t.Errorf("status = %d ต้องเป็น 401", got)
		}
	})

	t.Run("alg=none ต้องถูกปฏิเสธ", func(t *testing.T) {
		tok := jwt.NewWithClaims(jwt.SigningMethodNone, claims(time.Now().Add(time.Hour)))
		s, err := tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
		if err != nil {
			t.Fatalf("สร้าง token ไม่สำเร็จ: %v", err)
		}
		if got := status(t, app, "/admin/events", "Bearer "+s); got != fiber.StatusUnauthorized {
			t.Errorf("status = %d ต้องเป็น 401 — alg confusion ต้องกันได้", got)
		}
	})
}

// TestKeyRotation ระหว่าง rotate ต้องรับทั้งคีย์เก่าและใหม่
func TestKeyRotation(t *testing.T) {
	oldKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	newKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	app := protectedApp(t, []*rsa.PublicKey{&oldKey.PublicKey, &newKey.PublicKey}, RoleSuperAdmin)

	claims := jwt.MapClaims{
		"sub":   "af8ebb2e-1b91-4b12-9aa8-5424a2eb09b9",
		"roles": []string{RoleSuperAdmin},
		"exp":   time.Now().Add(time.Hour).Unix(),
	}

	for _, tc := range []struct {
		name    string
		key     *rsa.PrivateKey
		withKID bool
	}{
		{"คีย์เก่า มี kid", oldKey, true},
		{"คีย์เก่า ไม่มี kid (token ที่ออกก่อน rotate)", oldKey, false},
		{"คีย์ใหม่ มี kid", newKey, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tok := signToken(t, tc.key, claims, tc.withKID)
			if got := status(t, app, "/admin/events", "Bearer "+tok); got != fiber.StatusOK {
				t.Errorf("status = %d ต้องเป็น 200", got)
			}
		})
	}
}
