package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

func (s *memoryStore) listVenues(_ context.Context) []Venue {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Venue, 0, len(s.venues))
	for _, venue := range s.venues {
		out = append(out, venue)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (s *memoryStore) listSessions(_ context.Context, status string, page, pageSize int) ([]Session, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]Session, 0, len(s.sessions))
	for _, item := range s.sessions {
		if strings.TrimSpace(status) == "" || item.Status == status {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].StartsAt.Before(items[j].StartsAt) })
	total := len(items)
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	start := (page - 1) * pageSize
	if start >= len(items) {
		return []Session{}, total
	}
	end := start + pageSize
	if end > len(items) {
		end = len(items)
	}
	return append([]Session(nil), items[start:end]...), total
}

func (s *memoryStore) getSession(_ context.Context, id string) (Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.sessions[id]
	if !ok {
		return Session{}, errNotFound
	}
	return item, nil
}

func (s *memoryStore) createSession(_ context.Context, in Session) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(in.ID) == "" || strings.TrimSpace(in.VenueID) == "" || strings.TrimSpace(in.Title) == "" || in.StartsAt.IsZero() || in.EndsAt.IsZero() || !in.EndsAt.After(in.StartsAt) || in.Capacity <= 0 || in.Price < 0 {
		return Session{}, errInvalidInput
	}
	if _, ok := s.venues[in.VenueID]; !ok {
		return Session{}, errNotFound
	}
	if _, ok := s.sessions[in.ID]; ok {
		return Session{}, errDuplicate
	}
	now := time.Now().UTC()
	in.Status = SessionDraft
	in.CreatedAt = now
	in.UpdatedAt = now
	s.sessions[in.ID] = in
	s.appendSessionEventLocked(in.ID, "", SessionDraft, "系统", "创建场次", "")
	return in, nil
}

func (s *memoryStore) publishSession(_ context.Context, id, actor string) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.sessions[id]
	if !ok {
		return Session{}, errNotFound
	}
	if item.Status != SessionDraft {
		return Session{}, errDuplicate
	}
	from := item.Status
	item.Status = SessionScheduled
	item.UpdatedAt = time.Now().UTC()
	s.sessions[id] = item
	s.appendSessionEventLocked(id, from, item.Status, defaultActor(actor), "发布场次", "已通过排期审核")
	return item, nil
}

func (s *memoryStore) sellSession(_ context.Context, id string, quantity int, actor string) (Session, []Ticket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.sessions[id]
	if !ok {
		return Session{}, nil, errNotFound
	}
	if quantity <= 0 {
		return Session{}, nil, errInvalidInput
	}
	if item.Status != SessionScheduled && item.Status != SessionSelling {
		return Session{}, nil, errInvalidTransition
	}
	if item.Sold+quantity > item.Capacity {
		return Session{}, nil, errInventoryExceeded
	}
	if item.Status == SessionScheduled {
		from := item.Status
		item.Status = SessionSelling
		s.appendSessionEventLocked(id, from, item.Status, defaultActor(actor), "开始售票", "场次开放购票")
	}
	now := time.Now().UTC()
	tickets := make([]Ticket, 0, quantity)
	for i := 0; i < quantity; i++ {
		seq := item.Sold + i + 1
		code := fmt.Sprintf("%s-%04d", id, seq)
		ticket := Ticket{ID: fmt.Sprintf("T-%s-%04d", id, seq), SessionID: id, Code: code, Status: TicketAvailable, Price: item.Price, CreatedAt: now}
		s.tickets[code] = ticket
		tickets = append(tickets, ticket)
	}
	item.Sold += quantity
	item.UpdatedAt = now
	s.sessions[id] = item
	s.appendSessionEventLocked(id, item.Status, item.Status, defaultActor(actor), "售出门票", fmt.Sprintf("本次售出 %d 张", quantity))
	return item, tickets, nil
}

func (s *memoryStore) checkinSession(_ context.Context, id, code, actor string) (Ticket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.sessions[id]
	if !ok {
		return Ticket{}, errNotFound
	}
	ticket, ok := s.tickets[code]
	if !ok || ticket.SessionID != id {
		return Ticket{}, errNotFound
	}
	if ticket.Status == TicketCheckedIn {
		return Ticket{}, errDuplicate
	}
	if ticket.Status != TicketAvailable {
		return Ticket{}, errInvalidTransition
	}
	now := time.Now().UTC()
	ticket.Status = TicketCheckedIn
	ticket.CheckedInAt = &now
	s.tickets[code] = ticket
	item.CheckedIn++
	item.UpdatedAt = now
	s.sessions[id] = item
	s.appendSessionEventLocked(id, item.Status, item.Status, defaultActor(actor), "核销票码", "现场检票成功")
	return ticket, nil
}

func (s *memoryStore) transitionSession(_ context.Context, id, status, actor string) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.sessions[id]
	if !ok {
		return Session{}, errNotFound
	}
	if !allowedSessionTransition(item.Status, status) {
		return Session{}, errInvalidTransition
	}
	from := item.Status
	item.Status = status
	item.UpdatedAt = time.Now().UTC()
	s.sessions[id] = item
	s.appendSessionEventLocked(id, from, status, defaultActor(actor), "推进状态", "运营人员确认状态")
	return item, nil
}

func (s *memoryStore) settleSession(_ context.Context, id, actor string) (SessionSettlement, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.sessions[id]
	if !ok {
		return SessionSettlement{}, errNotFound
	}
	if item.Status != SessionPendingSettlement || time.Now().UTC().Before(item.EndsAt) || item.PendingExceptions > 0 {
		return SessionSettlement{}, errInvalidTransition
	}
	if existing, ok := s.sessionSettlements[id]; ok {
		return existing, errDuplicate
	}
	now := time.Now().UTC()
	item.Status = SessionSettled
	item.UpdatedAt = now
	s.sessions[id] = item
	settlement := SessionSettlement{ID: "SET-" + id, SessionID: id, TicketCount: item.Sold, Gross: float64(item.Sold) * item.Price, Status: "已结算", SettledAt: now}
	s.sessionSettlements[id] = settlement
	s.appendSessionEventLocked(id, SessionPendingSettlement, SessionSettled, defaultActor(actor), "完成日结", fmt.Sprintf("售出 %d 张，实收 %.2f", item.Sold, settlement.Gross))
	return settlement, nil
}

func (s *memoryStore) listSessionEvents(_ context.Context, id string) ([]SessionEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.sessions[id]; !ok {
		return nil, errNotFound
	}
	out := append([]SessionEvent(nil), s.sessionEvents[id]...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *memoryStore) appendSessionEventLocked(id, from, to, actor, action, detail string) {
	s.sessionEvents[id] = append(s.sessionEvents[id], SessionEvent{ID: s.nextSessionEventID, SessionID: id, Action: action, FromStatus: from, ToStatus: to, Actor: actor, Detail: detail, CreatedAt: time.Now().UTC()})
	s.nextSessionEventID++
}

func allowedSessionTransition(from, to string) bool {
	if from == to {
		return false
	}
	allowed := map[string]map[string]bool{
		SessionDraft:             {SessionScheduled: true},
		SessionScheduled:         {SessionSelling: true},
		SessionSelling:           {SessionActive: true, SessionPendingSettlement: true},
		SessionActive:            {SessionPendingSettlement: true},
		SessionPendingSettlement: {SessionSettled: true},
		SessionSettled:           {},
	}
	return allowed[from][to]
}

func defaultActor(actor string) string {
	if strings.TrimSpace(actor) == "" {
		return "运营人员"
	}
	return actor
}
