# vertex-event-service

บันทึก event ของระบบ vertex (audit log) — append-only ไม่มีการแก้และไม่มีการลบจากโค้ด

## endpoint

| method | path | ใครเรียก | ยืนยันตัวตนด้วย |
|---|---|---|---|
| `POST` | `/api/v1/events` | service อื่น (pet-service) | `X-Service-Token` |
| `GET`  | `/api/v1/admin/events` | backoffice | JWT + role `SUPER_ADMIN` |
| `GET`  | `/livez` `/readyz` `/health` | k8s | ไม่ต้อง |

### ทำไม POST ไม่ใช้ JWT ของผู้ใช้

การส่ง event เป็น fire-and-forget และต่อไปจะเป็น outbox ที่ส่งทีหลัง
token ของผู้ใช้อาจหมดอายุไปแล้วตอนที่ event ถูกส่งจริง
ส่วน "ใครทำ" (`actorId`) เป็นข้อมูล**ใน** event ไม่ใช่ตัวยืนยันสิทธิ์ของผู้เรียก

### idempotency

ผู้ส่งกำหนด `idempotencyKey` ได้ ส่งซ้ำด้วยคีย์เดิมจะได้ `200` พร้อม `"duplicate": true`
และไม่เกิดแถวใหม่ ทำให้ retry ปลอดภัย

## ตั้งค่า

ทุกค่าอ่านจาก environment และ **ไม่มี fallback** — ตั้งไม่ครบแล้วจะไม่ start
ดีกว่าไปต่อฐานข้อมูลผิดตัวเงียบๆ

| ตัวแปร | จำเป็น | ค่า default |
|---|---|---|
| `DB_USER` `DB_PASSWORD` | ✅ | — |
| `JWT_PUBLIC_KEYS` | ✅ | — (รับ PEM หลายบล็อกต่อกันเพื่อ rotate ได้) |
| `EVENT_INGEST_TOKEN` | ✅ | — (ต้องยาว ≥ 32 ตัวอักษร) |
| `DB_HOST` `DB_PORT` `DB_NAME` | | `localhost` `5432` `vertex` |
| `DB_SEARCH_PATH` | | `event` |
| `PORT` | | `4002` |
| `LOG_LEVEL` | | `info` |
| `SHUTDOWN_DRAIN_DELAY` `SHUTDOWN_TIMEOUT` | | `5s` `20s` |

## schema

จัดการโดย Flyway ใน repo [`vertex-migrations`](https://github.com/thepphithakp/vertex-migrations)
ที่โฟลเดอร์ `event/` — **ไม่มี AutoMigrate**

ตอน start จะตรวจ `flyway_schema_history` ว่า migration รันครบก่อนรับ request
ถ้ายังไม่ครบจะไม่ start พร้อมบอกเหตุผล แทนที่จะล้มเป็นราย request ทีหลัง

ต่อฐานข้อมูลด้วย `event_app` ซึ่งทำได้แค่ DML ใน schema `event`
มองไม่เห็นข้อมูลของ pet และ auth เลย

## พัฒนา

```
make help              # ดูคำสั่งทั้งหมด
make test              # unit test
make test-integration  # ต้องมี postgres ที่รัน migration ของ event แล้ว
make lint              # ใช้ docker ถ้าไม่มี golangci-lint ในเครื่อง
```

⚠️ integration test จะปฏิเสธการรันถ้าชื่อฐานข้อมูลไม่มีคำว่า `test`
เพื่อกันไม่ให้เผลอยิงใส่ production ตอนมี port-forward ค้างอยู่

## deploy

GitOps ด้วย Argo CD — GitHub Actions ไม่แตะ cluster
เมื่อ push ขึ้น `main` CI จะ build image แล้ว commit `image.tag` ใหม่เข้า
[`vertex-gitops`](https://github.com/thepphithakp/vertex-gitops) (`apps/event-service/values.yaml`)
จากนั้น Argo CD (https://argocd.pluem.me) sync เข้า namespace `vertex` เอง

ต้องมี secret ใน repo: `GITOPS_DEPLOY_KEY` (deploy key ที่เขียน vertex-gitops ได้) และ `MIGRATIONS_REPO_TOKEN`

⚠️ chart ที่ Argo ใช้คือสำเนาใน `vertex-gitops/charts/event-service` ถ้าแก้ `helm/event-service`
ต้องแก้ที่นั่นให้ตรงกันด้วย

rollback: revert commit ของ tag ใน `vertex-gitops`
