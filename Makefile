.DEFAULT_GOAL := help
GOLANGCI_VERSION ?= v2.13.1

help: ## แสดงคำสั่งทั้งหมด
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "};{printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

build: ## build binary
	go build -trimpath -o bin/event-service ./cmd/server

vet: ## go vet ทั้งแบบปกติและแบบรวม integration
	go vet ./...
	go vet -tags=integration ./...

lint: ## golangci-lint (ใช้ binary ในเครื่องถ้ามี ไม่มีก็ใช้ docker)
	@if command -v golangci-lint >/dev/null; then \
		golangci-lint run ./...; \
	else \
		echo "ไม่พบ golangci-lint ในเครื่อง — ใช้ docker $(GOLANGCI_VERSION) แทน"; \
		docker run --rm -v "$(PWD)":/app -w /app \
			golangci/golangci-lint:$(GOLANGCI_VERSION) golangci-lint run ./...; \
	fi

test: ## unit test (ไม่ต้องใช้ docker)
	go test -race -count=1 ./...

test-integration: ## integration test (ต้องมี postgres ที่รัน migration ของ event แล้ว)
	@echo "⚠️  ฐานข้อมูลปลายทางต้องมีคำว่า test ในชื่อ ไม่งั้นเทสต์จะปฏิเสธการรัน"
	TEST_DATABASE_URL="$${TEST_DATABASE_URL:-postgres://vertex:vertex@localhost:55432/eventtest?sslmode=disable&search_path=event}" \
	go test -race -count=1 -tags=integration ./...

tidy: ## go mod tidy + ตรวจว่าไม่มี diff
	go mod tidy
	@git diff --exit-code go.mod go.sum || { echo "go.mod/go.sum ไม่ tidy — commit การเปลี่ยนแปลงด้วย"; exit 1; }

docker-build: ## build image
	docker build -t event-service:local .

.PHONY: help build vet lint test test-integration tidy docker-build
