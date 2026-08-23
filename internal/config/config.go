package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config รวมค่าตั้งทั้งหมดที่อ่านจาก environment
//
// เดิม main.go อ่าน env เองและมี DSN พร้อมรหัสผ่าน hardcode ไว้เป็น fallback
// ซึ่งแปลว่าตั้งค่าผิดแล้วไม่พัง แต่ไปต่อ database ผิดตัวเงียบๆ
// ตรงนี้จึงบังคับให้ค่าที่จำเป็นต้องมี ไม่งั้นไม่ยอม start
type Config struct {
	Port     string
	DB       DBConfig
	JWT      JWTConfig
	Ingest   IngestConfig
	Log      LogConfig
	Shutdown ShutdownConfig
}

type DBConfig struct {
	Host       string
	Port       string
	User       string
	Password   string
	Name       string
	SSLMode    string
	SearchPath string
}

type JWTConfig struct {
	// PublicKeys รับ PEM หลายบล็อกต่อกัน เพื่อให้ rotate key ได้โดยไม่ downtime
	PublicKeys string
	Issuer     string
	Audience   string
}

// IngestConfig คุมการยืนยันตัวตนของ POST /events ซึ่งถูกเรียกโดย service อื่น
// ไม่ใช่โดยผู้ใช้ จึงใช้ token แบบ service-to-service ไม่ใช่ JWT ของผู้ใช้
type IngestConfig struct {
	Token string
}

type LogConfig struct {
	Level string
}

type ShutdownConfig struct {
	DrainDelay time.Duration
	Timeout    time.Duration
}

// DSN ประกอบ connection string ให้ GORM
func (c DBConfig) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s search_path=%s TimeZone=Asia/Bangkok",
		c.Host, c.Port, c.User, c.Password, c.Name, c.SSLMode, c.SearchPath,
	)
}

// Redacted ใช้ตอน log — ต้องไม่มีรหัสผ่านหลุดออกไป
func (c DBConfig) Redacted() string {
	return fmt.Sprintf("host=%s port=%s user=%s dbname=%s search_path=%s",
		c.Host, c.Port, c.User, c.Name, c.SearchPath)
}

// Load อ่าน config แล้วตรวจให้ครบก่อนคืนค่า
func Load() (Config, error) {
	cfg := Config{
		Port: env("PORT", "4002"),
		DB: DBConfig{
			Host:       env("DB_HOST", "localhost"),
			Port:       env("DB_PORT", "5432"),
			User:       os.Getenv("DB_USER"),
			Password:   os.Getenv("DB_PASSWORD"),
			Name:       env("DB_NAME", "vertex"),
			SSLMode:    env("DB_SSL_MODE", "disable"),
			SearchPath: env("DB_SEARCH_PATH", "event"),
		},
		JWT: JWTConfig{
			PublicKeys: os.Getenv("JWT_PUBLIC_KEYS"),
			Issuer:     os.Getenv("JWT_ISSUER"),
			Audience:   os.Getenv("JWT_AUDIENCE"),
		},
		Ingest: IngestConfig{
			Token: os.Getenv("EVENT_INGEST_TOKEN"),
		},
		Log: LogConfig{Level: env("LOG_LEVEL", "info")},
		Shutdown: ShutdownConfig{
			DrainDelay: envDuration("SHUTDOWN_DRAIN_DELAY", 5*time.Second),
			Timeout:    envDuration("SHUTDOWN_TIMEOUT", 20*time.Second),
		},
	}

	var missing []string
	if cfg.DB.User == "" {
		missing = append(missing, "DB_USER")
	}
	if cfg.DB.Password == "" {
		missing = append(missing, "DB_PASSWORD")
	}
	// ไม่มี public key = ตรวจ token ไม่ได้เลย ซึ่งแปลว่า endpoint admin
	// จะเปิดโล่งหรือใช้ไม่ได้ ทั้งสองแบบไม่ควรปล่อยให้ start ขึ้นเงียบๆ
	if cfg.JWT.PublicKeys == "" {
		missing = append(missing, "JWT_PUBLIC_KEYS")
	}
	if cfg.Ingest.Token == "" {
		missing = append(missing, "EVENT_INGEST_TOKEN")
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("ไม่ได้ตั้ง environment variable ที่จำเป็น: %s", strings.Join(missing, ", "))
	}

	// token สั้นเกินไปเดาได้ — ยาวอย่างน้อย 32 ตัวอักษร
	if len(cfg.Ingest.Token) < minIngestTokenLength {
		return Config{}, fmt.Errorf(
			"EVENT_INGEST_TOKEN สั้นเกินไป (%d ตัวอักษร) ต้องอย่างน้อย %d — สร้างด้วย: openssl rand -base64 48",
			len(cfg.Ingest.Token), minIngestTokenLength)
	}

	return cfg, nil
}

// minIngestTokenLength กันการตั้ง token สั้นๆ อย่าง "secret" ซึ่งเดาได้ในไม่กี่วินาที
const minIngestTokenLength = 32

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	if d, err := time.ParseDuration(v); err == nil {
		return d
	}
	// รับตัวเลขเปล่าเป็นวินาที เผื่อคนตั้งค่ามาแบบไม่มีหน่วย
	if n, err := strconv.Atoi(v); err == nil {
		return time.Duration(n) * time.Second
	}
	return fallback
}
