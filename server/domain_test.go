package main

import (
	"context"
	"errors"
	"testing"
)

func TestShipmentLifecycle(t *testing.T) {
	store := newMemoryStore()
	ctx := context.Background()
	created, err := store.createShipment(ctx, Shipment{ID: "FF-TEST-001", Route: "浦东 → 静安", Cargo: "生鲜 2 箱", Status: ShipmentPendingDispatch})
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != ShipmentPendingDispatch {
		t.Fatalf("initial status = %q", created.Status)
	}
	if _, err = store.assignShipment(ctx, created.ID, "周师傅", "调度主管"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.transitionShipment(ctx, created.ID, ShipmentInTransit, "场馆工作人员", "已接单出发"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.transitionShipment(ctx, created.ID, ShipmentDelivered, "场馆工作人员", "活动已完成撤场"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.transitionShipment(ctx, created.ID, ShipmentCompleted, "收货人", "已签收"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.transitionShipment(ctx, created.ID, ShipmentInTransit, "场馆工作人员", "非法回退"); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("expected invalid transition, got %v", err)
	}
	events, err := store.listShipmentEvents(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 5 {
		t.Fatalf("events = %d, want 5", len(events))
	}
}

func TestIdempotencyStoreReturnsSameResult(t *testing.T) {
	store := newMemoryIdempotency()
	ctx := context.Background()
	if err := store.Set(ctx, "shipment:create:abc", `{"id":"FF-1"}`); err != nil {
		t.Fatal(err)
	}
	got, ok, err := store.Get(ctx, "shipment:create:abc")
	if err != nil || !ok || got != `{"id":"FF-1"}` {
		t.Fatalf("got %q, %v, %v", got, ok, err)
	}
}

func TestShipmentCannotAssignCompleted(t *testing.T) {
	store := newMemoryStore()
	ctx := context.Background()
	_, _ = store.createShipment(ctx, Shipment{ID: "FF-TEST-002", Status: ShipmentCompleted})
	if _, err := store.assignShipment(ctx, "FF-TEST-002", "周师傅", "调度主管"); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("expected invalid transition, got %v", err)
	}
}
