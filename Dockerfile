# --- Build ---
FROM golang:1.25.14-alpine AS builder
RUN apk add --no-cache git ca-certificates tzdata
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath -ldflags="-s -w" \
    -o /out/event-service ./cmd/server

# --- Run ---
# distroless แทน alpine: ได้ ca-certificates, tzdata และ user nonroot มาให้
#
# ของเดิมใช้ alpine + WORKDIR /root/ ซึ่งรันเป็น UID 0 และวาง binary ไว้ใน
# home ของ root — พอ helm สั่ง runAsUser 65532 จะอ่านไฟล์ไม่ได้เลย
FROM gcr.io/distroless/static:nonroot

COPY --from=builder /out/event-service /app/event-service

WORKDIR /app
USER nonroot:nonroot
EXPOSE 4002

ENTRYPOINT ["/app/event-service"]
