package model

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/vertex/event-service/internal/domain"
)

func TestEventLogRow_RoundTrip(t *testing.T) {
	key := "idem-1"
	want := domain.EventLog{
		ID:             uuid.New(),
		Timestamp:      time.Now().Truncate(time.Second),
		CreatedAt:      time.Now().Truncate(time.Second),
		EventType:      "pet.created",
		Action:         "create",
		ActorID:        "u1",
		ActorUsername:  "somchai",
		EntityID:       "pet-1",
		EntityType:     "pet",
		Payload:        []byte(`{"name":"มะลิ"}`),
		IdempotencyKey: &key,
	}

	row := EventLogRowFromDomain(want)
	got := row.ToDomain()

	if got.ID != want.ID || got.EventType != want.EventType || got.ActorID != want.ActorID {
		t.Fatalf("round trip ไม่ตรง: %+v", got)
	}
	if string(got.Payload) != string(want.Payload) {
		t.Fatalf("Payload ไม่ตรง: got %q want %q", got.Payload, want.Payload)
	}
	if got.IdempotencyKey == nil || *got.IdempotencyKey != key {
		t.Fatalf("IdempotencyKey ไม่ตรง: %v", got.IdempotencyKey)
	}
}

func TestEventLogRow_NilIdempotencyKeyStaysNil(t *testing.T) {
	row := EventLogRowFromDomain(domain.EventLog{IdempotencyKey: nil})
	if row.IdempotencyKey != nil {
		t.Fatalf("nil ต้องคง nil ไม่ใช่ pointer ไปที่ empty string")
	}
	got := row.ToDomain()
	if got.IdempotencyKey != nil {
		t.Fatalf("nil ต้องคง nil หลัง round trip")
	}
}
