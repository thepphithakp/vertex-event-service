package repository

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/vertex/event-service/internal/adapter/repository/model"
	"github.com/vertex/event-service/internal/domain"
	"github.com/vertex/event-service/internal/port"
)

type GORMEventRepository struct {
	db *gorm.DB
}

// NewGORMEventRepository สร้าง repository
//
// 🔸 ไม่มี AutoMigrate แล้ว — schema จัดการโดย Flyway
//
//	ของเดิมเรียก db.AutoMigrate(&domain.EventLog{}) ตรงนี้ ซึ่งแปลว่า
//	โครงตารางบน production ขึ้นกับโค้ดที่ deploy ล่าสุด ไม่มีประวัติ
//	ไม่มีทาง review และย้อนกลับไม่ได้ นอกจากนี้ event_app ยังถูกถอด
//	สิทธิ์ DDL ออกแล้ว ต่อให้เรียกก็ทำไม่ได้
func NewGORMEventRepository(db *gorm.DB) *GORMEventRepository {
	return &GORMEventRepository{db: db}
}

// Save เขียน event แบบ idempotent
//
// ถ้ามี IdempotencyKey แล้วชนกับที่มีอยู่ จะไม่เกิดแถวใหม่และคืน created=false
// ใช้ ON CONFLICT DO NOTHING แทนการ SELECT ก่อนแล้วค่อย INSERT
// เพราะสองคำสั่งแยกกันมีช่องให้ผู้เรียกสองรายแทรกพร้อมกันได้
//
// 🔴 เขียนทับ *event ด้วยค่าที่ GORM เติมกลับมาหลัง insert (เช่น CreatedAt
// จาก DB default) — caller (handler) พึ่งพาพฤติกรรมนี้อยู่ เพื่อให้ response
// มีค่าที่ DB ใส่จริง ไม่ใช่ zero value ที่ยังไม่ผ่าน insert
func (r *GORMEventRepository) Save(ctx context.Context, event *domain.EventLog) (bool, error) {
	row := model.EventLogRowFromDomain(*event)

	// ⚠️ ต้องระบุ TargetWhere ให้ตรงกับ WHERE ของ unique index
	//
	// ux_event_logs_idempotency_key เป็น partial index (WHERE ... IS NOT NULL)
	// PostgreSQL จับคู่ ON CONFLICT กับ index ด้วย "คอลัมน์ + เงื่อนไข"
	// ถ้าใส่แค่คอลัมน์จะได้ error ว่าไม่มี unique constraint ที่ตรงกัน
	// แล้วทุก INSERT กลายเป็น 500 (เทสต์ integration จับได้)
	tx := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "idempotency_key"}},
			TargetWhere: clause.Where{Exprs: []clause.Expression{
				clause.Expr{SQL: "idempotency_key IS NOT NULL"},
			}},
			DoNothing: true,
		}).
		Create(&row)

	if tx.Error != nil {
		return false, tx.Error
	}
	*event = row.ToDomain()
	return tx.RowsAffected > 0, nil
}

func (r *GORMEventRepository) List(ctx context.Context, f port.ListFilter) ([]domain.EventLog, error) {
	var rows []model.EventLogRow
	if err := r.filter(ctx, f).
		Order("timestamp desc").
		Limit(f.Limit).
		Offset(f.Offset).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	events := make([]domain.EventLog, 0, len(rows))
	for _, row := range rows {
		events = append(events, row.ToDomain())
	}
	return events, nil
}

func (r *GORMEventRepository) Count(ctx context.Context, f port.ListFilter) (int64, error) {
	var n int64
	err := r.filter(ctx, f).Model(&model.EventLogRow{}).Count(&n).Error
	return n, err
}

// filter รวมเงื่อนไขไว้ที่เดียว ให้ List กับ Count นับจากชุดเดียวกันเสมอ
func (r *GORMEventRepository) filter(ctx context.Context, f port.ListFilter) *gorm.DB {
	q := r.db.WithContext(ctx).Model(&model.EventLogRow{})
	if f.EntityType != "" {
		q = q.Where("entity_type = ?", f.EntityType)
	}
	if f.EntityID != "" {
		q = q.Where("entity_id = ?", f.EntityID)
	}
	if f.ActorID != "" {
		q = q.Where("actor_id = ?", f.ActorID)
	}
	return q
}
