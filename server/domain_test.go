package main

import (
	"context"
	"errors"
	"testing"
	"time"
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

func TestSessionRejectsInvalidTimeRangeAndCapacity(t *testing.T) {
	store := newMemoryStore()
	ctx := context.Background()
	if _, err := store.createSession(ctx, Session{ID: "VS-TEST-001", VenueID: "VEN-001", Title: "演示音乐节", StartsAt: time.Date(2026, 7, 20, 18, 0, 0, 0, time.UTC), EndsAt: time.Date(2026, 7, 20, 17, 0, 0, 0, time.UTC), Capacity: 100, Price: 88}); !errors.Is(err, errInvalidInput) {
		t.Fatalf("expected invalid time range, got %v", err)
	}
	if _, err := store.createSession(ctx, Session{ID: "VS-TEST-002", VenueID: "VEN-001", Title: "演示音乐节", StartsAt: time.Date(2026, 7, 20, 18, 0, 0, 0, time.UTC), EndsAt: time.Date(2026, 7, 20, 20, 0, 0, 0, time.UTC), Capacity: 0, Price: 88}); !errors.Is(err, errInvalidInput) {
		t.Fatalf("expected invalid capacity, got %v", err)
	}
}

func TestSessionInventoryCheckinAndSettlementGuards(t *testing.T) {
	store := newMemoryStore()
	ctx := context.Background()
	start := time.Now().UTC().Add(-2 * time.Hour)
	end := time.Now().UTC().Add(2 * time.Hour)
	session, err := store.createSession(ctx, Session{ID: "VS-TEST-003", VenueID: "VEN-001", Title: "演示展览", StartsAt: start, EndsAt: end, Capacity: 2, Price: 128})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.publishSession(ctx, session.ID, "运营主管"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.sellSession(ctx, session.ID, 3, "运营主管"); !errors.Is(err, errInventoryExceeded) {
		t.Fatalf("expected over-sale guard, got %v", err)
	}
	sold, tickets, err := store.sellSession(ctx, session.ID, 2, "运营主管")
	if err != nil || len(tickets) != 2 {
		t.Fatalf("sell = %+v tickets=%d err=%v", sold, len(tickets), err)
	}
	if _, err := store.checkinSession(ctx, session.ID, tickets[0].Code, "现场工作人员"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.checkinSession(ctx, session.ID, tickets[0].Code, "现场工作人员"); !errors.Is(err, errDuplicate) {
		t.Fatalf("expected duplicate check-in, got %v", err)
	}
	if _, err := store.settleSession(ctx, session.ID, "运营主管"); !errors.Is(err, errInvalidTransition) {
		t.Fatalf("expected settlement-before-end guard, got %v", err)
	}
	store.sessions[session.ID] = func() Session {
		item, _ := store.getSession(ctx, session.ID)
		item.EndsAt = time.Now().UTC().Add(-time.Hour)
		item.Status = SessionPendingSettlement
		return item
	}()
	if _, err := store.settleSession(ctx, session.ID, "运营主管"); err != nil {
		t.Fatalf("settlement after end = %v", err)
	}
}

func TestSessionEventsRemainOrderedAndPublishIsIdempotent(t *testing.T) {
	store := newMemoryStore()
	ctx := context.Background()
	now := time.Now().UTC()
	session, err := store.createSession(ctx, Session{ID: "VS-TEST-004", VenueID: "VEN-001", Title: "演示论坛", StartsAt: now.Add(time.Hour), EndsAt: now.Add(3 * time.Hour), Capacity: 10, Price: 66})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.publishSession(ctx, session.ID, "运营主管"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.publishSession(ctx, session.ID, "运营主管"); !errors.Is(err, errDuplicate) {
		t.Fatalf("expected duplicate publish, got %v", err)
	}
	events, err := store.listSessionEvents(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Action != "创建场次" || events[1].Action != "发布场次" || events[0].CreatedAt.After(events[1].CreatedAt) {
		t.Fatalf("events=%+v", events)
	}
}
