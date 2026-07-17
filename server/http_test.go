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

func TestSessionRoutesValidateInventoryAndCheckin(t *testing.T) {
	r := (&app{store: newMemoryStore(), idem: newMemoryRuntimeIdem()}).routes()
	body := `{"venueId":"VEN-001","title":"演示展览","startsAt":"2026-07-20T18:00:00Z","endsAt":"2026-07-20T20:00:00Z","capacity":2,"price":128}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "session-http")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", w.Code, w.Body.String())
	}
	var envelope struct {
		Data Session `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	id := envelope.Data.ID
	publish := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/"+id+"/publish", bytes.NewBufferString(`{"actor":"运营主管"}`))
	publish.Header.Set("Content-Type", "application/json")
	publish.Header.Set("Idempotency-Key", "publish-http")
	wp := httptest.NewRecorder()
	r.ServeHTTP(wp, publish)
	if wp.Code != http.StatusOK {
		t.Fatalf("publish status=%d body=%s", wp.Code, wp.Body.String())
	}
	sell := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/"+id+"/sell", bytes.NewBufferString(`{"quantity":1,"actor":"运营主管"}`))
	sell.Header.Set("Content-Type", "application/json")
	sell.Header.Set("Idempotency-Key", "sell-http")
	ws := httptest.NewRecorder()
	r.ServeHTTP(ws, sell)
	if ws.Code != http.StatusOK {
		t.Fatalf("sell status=%d body=%s", ws.Code, ws.Body.String())
	}
	var sold struct {
		Data struct {
			Tickets []Ticket `json:"tickets"`
		} `json:"data"`
	}
	if err := json.Unmarshal(ws.Body.Bytes(), &sold); err != nil {
		t.Fatal(err)
	}
	if len(sold.Data.Tickets) != 1 {
		t.Fatalf("tickets=%+v", sold.Data.Tickets)
	}
	checkin := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/"+id+"/checkin", bytes.NewBufferString(`{"ticketCode":"`+sold.Data.Tickets[0].Code+`","actor":"现场工作人员"}`))
	checkin.Header.Set("Content-Type", "application/json")
	checkin.Header.Set("Idempotency-Key", "checkin-http")
	wc := httptest.NewRecorder()
	r.ServeHTTP(wc, checkin)
	if wc.Code != http.StatusOK {
		t.Fatalf("checkin status=%d body=%s", wc.Code, wc.Body.String())
	}
	duplicate := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/"+id+"/checkin", bytes.NewBufferString(`{"ticketCode":"`+sold.Data.Tickets[0].Code+`","actor":"现场工作人员"}`))
	duplicate.Header.Set("Content-Type", "application/json")
	duplicate.Header.Set("Idempotency-Key", "checkin-http-2")
	wd := httptest.NewRecorder()
	r.ServeHTTP(wd, duplicate)
	if wd.Code != http.StatusConflict {
		t.Fatalf("duplicate status=%d body=%s", wd.Code, wd.Body.String())
	}
}
