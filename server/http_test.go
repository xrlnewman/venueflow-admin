package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateShipmentIsIdempotent(t *testing.T) {
	r := (&app{store: newMemoryStore(), idem: newMemoryRuntimeIdem()}).routes()
	body, _ := json.Marshal(map[string]string{"route": "浦东 → 静安", "cargo": "测试活动物料", "eta": "16:30"})
	request := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/shipments", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "http-test-001")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	first, second := request(), request()
	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("status = %d, %d", first.Code, second.Code)
	}
	var a, b struct {
		Data Shipment `json:"data"`
	}
	if json.Unmarshal(first.Body.Bytes(), &a) != nil || json.Unmarshal(second.Body.Bytes(), &b) != nil {
		t.Fatal("invalid response")
	}
	if a.Data.ID == "" || a.Data.ID != b.Data.ID {
		t.Fatalf("idempotency mismatch: %q vs %q", a.Data.ID, b.Data.ID)
	}
}

func TestInvalidTransitionReturnsConflict(t *testing.T) {
	store := newMemoryStore()
	_, _ = store.createShipment(context.Background(), Shipment{ID: "FF-HTTP-001", Status: ShipmentCompleted})
	r := (&app{store: store, idem: newMemoryRuntimeIdem()}).routes()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/shipments/FF-HTTP-001/status", bytes.NewBufferString(`{"status":"现场服务中"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "http-test-transition")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
}
