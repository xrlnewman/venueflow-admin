package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

const (
	ShipmentPendingDispatch = "待预订"
	ShipmentPendingAccept   = "已锁场"
	ShipmentInTransit       = "进行中"
	ShipmentDelivered       = "待结算"
	ShipmentCompleted       = "已完成"
	ShipmentCancelled       = "已取消"
)

var (
	errNotFound          = errors.New("资源不存在")
	errInvalidTransition = errors.New("状态流转不合法")
	errDuplicate         = errors.New("资源已存在")
)

type Shipment struct {
	ID        string    `json:"id"`
	Route     string    `json:"route"`
	Cargo     string    `json:"cargo"`
	Driver    string    `json:"driver"`
	Vehicle   string    `json:"vehicle"`
	ETA       string    `json:"eta"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ShipmentEvent struct {
	ID         int64     `json:"id"`
	ShipmentID string    `json:"shipmentId"`
	FromStatus string    `json:"fromStatus"`
	ToStatus   string    `json:"toStatus"`
	Actor      string    `json:"actor"`
	Note       string    `json:"note"`
	CreatedAt  time.Time `json:"createdAt"`
}

type Driver struct{ ID, Name, Phone, Vehicle, Status string }
type Vehicle struct{ ID, Plate, Type, Status string }
type Exception struct {
	ID, ShipmentID, Type, Text, Level, Status string
	CreatedAt, ResolvedAt                     *time.Time
}
type Settlement struct {
	ID, Period, Status         string
	DriverCount, ShipmentCount int
	Amount                     float64
}

type memoryStore struct {
	mu          sync.RWMutex
	shipments   map[string]Shipment
	events      map[string][]ShipmentEvent
	exceptions  map[string]Exception
	drivers     []Driver
	vehicles    []Vehicle
	settlements []Settlement
	nextEventID int64
}

func newMemoryStore() *memoryStore {
	s := &memoryStore{shipments: map[string]Shipment{}, events: map[string][]ShipmentEvent{}, exceptions: map[string]Exception{}, nextEventID: 1}
	s.drivers = []Driver{{"D-001", "周协调员", "13800000001", "A 厅 · 500 人", "进行中"}, {"D-002", "陈协调员", "13800000002", "B 厅 · 300 人", "进行中"}, {"D-003", "林协调员", "13800000003", "草坪 · 800 人", "休息中"}, {"D-004", "王协调员", "13800000004", "剧场 · 600 人", "进行中"}, {"D-005", "赵协调员", "13800000005", "会议室 · 80 人", "进行中"}, {"D-006", "孙协调员", "13800000006", "露台 · 120 人", "进行中"}}
	s.vehicles = []Vehicle{{"V-001", "A 厅", "会展中心", "在线"}, {"V-002", "B 厅", "艺术馆", "在线"}, {"V-003", "草坪", "城市公园", "维护"}, {"V-004", "剧场", "文化中心", "在线"}}
	now := time.Now().UTC()
	for i := 0; i < 12; i++ {
		id := fmt.Sprintf("FF-%s-%03d", now.Format("060102"), 18-i)
		status := ShipmentPendingDispatch
		if i < 2 {
			status = ShipmentInTransit
		} else if i == 2 {
			status = ShipmentPendingAccept
		} else if i == 3 {
			status = ShipmentCompleted
		}
		_, _ = s.createShipment(context.Background(), Shipment{ID: id, Route: fmt.Sprintf("%s → %s", []string{"会展中心", "艺术馆", "城市公园", "文化中心"}[i%4], []string{"A 厅", "B 厅", "草坪", "剧场"}[(i+1)%4]), Cargo: fmt.Sprintf("品牌发布会 · %d 人", 80+i*20), Driver: s.drivers[i%len(s.drivers)].Name, Vehicle: s.drivers[i%len(s.drivers)].Vehicle, ETA: fmt.Sprintf("%02d:%02d", 14+i/2, (i%2)*30), Status: status, CreatedAt: now.Add(-time.Duration(i) * time.Hour)})
	}
	for i, e := range []Exception{{"EX-041", "FF-" + now.Format("060102") + "-018", "设备告警", "主会场音响需要复检", "高", "待处理", &now, nil}, {"EX-040", "FF-" + now.Format("060102") + "-017", "布场确认", "签到台物料尚未入场", "中", "待处理", &now, nil}, {"EX-039", "FF-" + now.Format("060102") + "-016", "场地资源告警", "需要确认延迟撤场服务", "低", "待处理", &now, nil}} {
		e.ID = fmt.Sprintf("EX-%03d", 41-i)
		s.exceptions[e.ID] = e
	}
	s.settlements = []Settlement{{"SET-2026-07-01", "07/01 - 07/07", "已结算", 38, 386, 24680}, {"SET-2026-07-08", "07/08 - 07/14", "已结算", 42, 428, 31220}, {"SET-2026-07-15", "07/15 - 07/21", "待确认", 40, 198, 12680}}
	return s
}

func (s *memoryStore) createShipment(_ context.Context, in Shipment) (Shipment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if in.ID == "" {
		return Shipment{}, errors.New("活动订单号不能为空")
	}
	if _, ok := s.shipments[in.ID]; ok {
		return Shipment{}, errDuplicate
	}
	if in.Status == "" {
		in.Status = ShipmentPendingDispatch
	}
	if in.CreatedAt.IsZero() {
		in.CreatedAt = time.Now().UTC()
	}
	in.UpdatedAt = in.CreatedAt
	s.shipments[in.ID] = in
	s.appendEventLocked(in.ID, "", in.Status, "系统", "创建活动订单")
	return in, nil
}

func (s *memoryStore) appendEventLocked(id, from, to, actor, note string) {
	s.events[id] = append(s.events[id], ShipmentEvent{ID: s.nextEventID, ShipmentID: id, FromStatus: from, ToStatus: to, Actor: actor, Note: note, CreatedAt: time.Now().UTC()})
	s.nextEventID++
}

func allowedShipmentTransition(from, to string) bool {
	if from == to {
		return true
	}
	allowed := map[string]map[string]bool{ShipmentPendingDispatch: {ShipmentPendingAccept: true, ShipmentCancelled: true}, ShipmentPendingAccept: {ShipmentInTransit: true, ShipmentCancelled: true}, ShipmentInTransit: {ShipmentDelivered: true, ShipmentCancelled: true}, ShipmentDelivered: {ShipmentCompleted: true}, ShipmentCompleted: {}, ShipmentCancelled: {}}
	return allowed[from][to]
}

func (s *memoryStore) assignShipment(_ context.Context, id, driver, actor string) (Shipment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.shipments[id]
	if !ok {
		return Shipment{}, errNotFound
	}
	if item.Status != ShipmentPendingDispatch && item.Status != ShipmentPendingAccept {
		return Shipment{}, errInvalidTransition
	}
	item.Driver = driver
	item.Status = ShipmentPendingAccept
	item.UpdatedAt = time.Now().UTC()
	s.shipments[id] = item
	s.appendEventLocked(id, ShipmentPendingDispatch, ShipmentPendingAccept, actor, "已分配场馆协调员")
	return item, nil
}

func (s *memoryStore) transitionShipment(_ context.Context, id, status, actor, note string) (Shipment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.shipments[id]
	if !ok {
		return Shipment{}, errNotFound
	}
	if !allowedShipmentTransition(item.Status, status) {
		return Shipment{}, errInvalidTransition
	}
	from := item.Status
	item.Status = status
	item.UpdatedAt = time.Now().UTC()
	s.shipments[id] = item
	s.appendEventLocked(id, from, status, actor, note)
	return item, nil
}

func (s *memoryStore) getShipment(_ context.Context, id string) (Shipment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.shipments[id]
	if !ok {
		return Shipment{}, errNotFound
	}
	return v, nil
}
func (s *memoryStore) listShipmentEvents(_ context.Context, id string) ([]ShipmentEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.shipments[id]; !ok {
		return nil, errNotFound
	}
	out := append([]ShipmentEvent(nil), s.events[id]...)
	return out, nil
}
func (s *memoryStore) listShipments(_ context.Context, status string, page, pageSize int) ([]Shipment, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	all := make([]Shipment, 0, len(s.shipments))
	for _, v := range s.shipments {
		if status == "" || v.Status == status {
			all = append(all, v)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].UpdatedAt.After(all[j].UpdatedAt) })
	total := len(all)
	start := (page - 1) * pageSize
	if start >= total {
		return []Shipment{}, total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return all[start:end], total
}
func (s *memoryStore) listDrivers(_ context.Context) []Driver {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Driver(nil), s.drivers...)
}
func (s *memoryStore) listVehicles(_ context.Context) []Vehicle {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Vehicle(nil), s.vehicles...)
}
func (s *memoryStore) listExceptions(_ context.Context, status string) []Exception {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []Exception{}
	for _, v := range s.exceptions {
		if status == "" || v.Status == status {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt == nil {
			return false
		}
		if out[j].CreatedAt == nil {
			return true
		}
		return out[i].CreatedAt.After(*out[j].CreatedAt)
	})
	return out
}
func (s *memoryStore) resolveException(_ context.Context, id string) (Exception, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.exceptions[id]
	if !ok {
		return Exception{}, errNotFound
	}
	if v.Status == "已处理" {
		return v, nil
	}
	now := time.Now().UTC()
	v.Status = "已处理"
	v.ResolvedAt = &now
	s.exceptions[id] = v
	return v, nil
}
func (s *memoryStore) listSettlements(_ context.Context) []Settlement {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Settlement(nil), s.settlements...)
}
func (s *memoryStore) confirmSettlement(_ context.Context, id string) (Settlement, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, v := range s.settlements {
		if v.ID == id {
			v.Status = "已结算"
			s.settlements[i] = v
			return v, nil
		}
	}
	return Settlement{}, errNotFound
}

type memoryIdempotency struct {
	mu     sync.Mutex
	values map[string]string
}

func newMemoryIdempotency() *memoryIdempotency {
	return &memoryIdempotency{values: map[string]string{}}
}
func (m *memoryIdempotency) Get(_ context.Context, key string) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.values[key]
	return v, ok, nil
}
func (m *memoryIdempotency) Set(_ context.Context, key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.values[key]; !ok {
		m.values[key] = value
	}
	return nil
}
