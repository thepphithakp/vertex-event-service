package middleware

import (
	"crypto/rsa"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
)

// role ที่ระบบรู้จัก — ต้องตรงกับที่ auth-service ใส่ใน claim "roles"
const (
	RoleSuperAdmin = "SUPER_ADMIN"
	RolePetAdmin   = "PET_ADMIN"
	RoleUser       = "USER"
)

// AuthConfig ควบคุมการตรวจ token ของผู้ใช้
type AuthConfig struct {
	PublicKeys []*rsa.PublicKey

	// Issuer / Audience ตรวจเมื่อไม่ว่างเท่านั้น
	//
	// ⚠️ เปิดได้ก็ต่อเมื่อ auth-service ออก iss/aud ครบแล้วและผ่านไป
	// นานกว่าอายุ token เดิม ไม่งั้น token ที่ผู้ใช้ถืออยู่จะใช้ไม่ได้ทันที
	Issuer   string
	Audience string
}

// Actor คือผู้เรียกที่ผ่านการตรวจ token แล้ว
type Actor struct {
	UserID string
	Email  string
	Name   string
	Roles  []string
}

// HasRole บอกว่า actor มี role นี้ไหม
func (a Actor) HasRole(role string) bool {
	for _, r := range a.Roles {
		if r == role {
			return true
		}
	}
	return false
}

const actorLocalsKey = "actor"

// ActorFrom ดึง actor ที่ middleware ผูกไว้กับ request
func ActorFrom(c *fiber.Ctx) (Actor, bool) {
	a, ok := c.Locals(actorLocalsKey).(Actor)
	return a, ok
}

// NewAuth ตรวจ JWT แล้วผูก Actor เข้ากับ request
func NewAuth(cfg AuthConfig) fiber.Handler {
	opts := []jwt.ParserOption{
		// defense-in-depth คู่กับการเช็ค method ใน keyfunc — กัน alg confusion
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithLeeway(30 * time.Second),
	}
	if cfg.Issuer != "" {
		opts = append(opts, jwt.WithIssuer(cfg.Issuer))
	}
	if cfg.Audience != "" {
		opts = append(opts, jwt.WithAudience(cfg.Audience))
	}

	keys := newKeySet(cfg.PublicKeys)

	return func(c *fiber.Ctx) error {
		raw, ok := bearerToken(c.Get(fiber.HeaderAuthorization))
		if !ok {
			return unauthorized(c, "ต้องมี Authorization: Bearer <token>")
		}

		token, err := jwt.Parse(raw, keys.keyfunc, opts...)
		if err != nil || !token.Valid {
			return unauthorized(c, "token ไม่ถูกต้องหรือหมดอายุ")
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			return unauthorized(c, "claim ใน token ไม่ถูกต้อง")
		}

		sub, ok := claims["sub"].(string)
		if !ok || sub == "" {
			return unauthorized(c, "token ไม่มี subject")
		}

		name, _ := claims["name"].(string)
		email, _ := claims["email"].(string)

		c.Locals(actorLocalsKey, Actor{
			UserID: sub,
			Email:  email,
			Name:   name,
			Roles:  rolesFromClaims(claims),
		})
		c.Locals("userId", sub)
		return c.Next()
	}
}

// RequireRole ปล่อยผ่านเฉพาะ actor ที่มี role ตามที่กำหนด
//
// ต้องวางไว้หลัง NewAuth เสมอ ถ้าไม่มี actor แปลว่าต่อ middleware ผิดลำดับ
// จึงตอบ 401 ไม่ใช่ 403 — และไม่ปล่อยผ่านเด็ดขาด
func RequireRole(roles ...string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		actor, ok := ActorFrom(c)
		if !ok {
			return unauthorized(c, "ยังไม่ได้ยืนยันตัวตน")
		}
		for _, r := range roles {
			if actor.HasRole(r) {
				return c.Next()
			}
		}
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error":     "ไม่มีสิทธิ์เข้าถึงข้อมูลนี้",
			"requestId": c.Get(HeaderRequestID),
		})
	}
}

// rolesFromClaims อ่าน roles จาก token
//
// token ที่ไม่มี roles ถือเป็น USER ธรรมดา ไม่ใช่ "ไม่มี role"
// เพื่อให้พฤติกรรมตรงกับ pet-service และไม่มีทางกลายเป็นสิทธิ์สูงโดยบังเอิญ
func rolesFromClaims(claims jwt.MapClaims) []string {
	var roles []string
	if raw, ok := claims["roles"].([]interface{}); ok {
		for _, r := range raw {
			if s, ok := r.(string); ok && s != "" {
				roles = append(roles, s)
			}
		}
	}
	if len(roles) == 0 {
		return []string{RoleUser}
	}
	return roles
}

// bearerToken ดึง token ออกจาก Authorization header
// RFC 7235 บอกว่า scheme เป็น case-insensitive
func bearerToken(header string) (string, bool) {
	const prefix = "bearer "
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return "", false
	}
	token := strings.TrimSpace(header[len(prefix):])
	return token, token != ""
}

func unauthorized(c *fiber.Ctx, msg string) error {
	return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
		"error":     msg,
		"requestId": c.Get(HeaderRequestID),
	})
}
