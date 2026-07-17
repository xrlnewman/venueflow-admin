package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type sqlStore struct{ db *sql.DB }

func (s *sqlStore) listVenues(ctx context.Context) []Venue {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, address, capacity, status FROM venues ORDER BY id`)
	if err != nil {
		return []Venue{}
	}
	defer rows.Close()
	out := []Venue{}
	for rows.Next() {
		var v Venue
		if rows.Scan(&v.ID, &v.Name, &v.Address, &v.Capacity, &v.Status) == nil {
			out = append(out, v)
		}
	}
	return out
}

func (s *sqlStore) listSessions(ctx context.Context, status string, page, pageSize int) ([]Session, int) {
	var total int
	count := `SELECT COUNT(*) FROM sessions`
	args := []any{}
	if strings.TrimSpace(status) != "" {
		count += ` WHERE status = ?`
		args = append(args, status)
	}
	if err := s.db.QueryRowContext(ctx, count, args...).Scan(&total); err != nil {
		return []Session{}, 0
	}
	query := `SELECT id, venue_id, title, starts_at, ends_at, capacity, price, sold, checked_in, pending_exceptions, status, created_at, updated_at FROM sessions`
	args = []any{}
	if strings.TrimSpace(status) != "" {
		query += ` WHERE status = ?`
		args = append(args, status)
	}
	query += ` ORDER BY starts_at LIMIT ? OFFSET ?`
	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return []Session{}, total
	}
	defer rows.Close()
	out := []Session{}
	for rows.Next() {
		var v Session
		if rows.Scan(&v.ID, &v.VenueID, &v.Title, &v.StartsAt, &v.EndsAt, &v.Capacity, &v.Price, &v.Sold, &v.CheckedIn, &v.PendingExceptions, &v.Status, &v.CreatedAt, &v.UpdatedAt) == nil {
			out = append(out, v)
		}
	}
	return out, total
}

func (s *sqlStore) getSession(ctx context.Context, id string) (Session, error) {
	var v Session
	err := s.db.QueryRowContext(ctx, `SELECT id, venue_id, title, starts_at, ends_at, capacity, price, sold, checked_in, pending_exceptions, status, created_at, updated_at FROM sessions WHERE id = ?`, id).Scan(&v.ID, &v.VenueID, &v.Title, &v.StartsAt, &v.EndsAt, &v.Capacity, &v.Price, &v.Sold, &v.CheckedIn, &v.PendingExceptions, &v.Status, &v.CreatedAt, &v.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, errNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("查询场次: %w", err)
	}
	return v, nil
}

func (s *sqlStore) createSession(ctx context.Context, in Session) (Session, error) {
	if strings.TrimSpace(in.ID) == "" || strings.TrimSpace(in.VenueID) == "" || strings.TrimSpace(in.Title) == "" || in.StartsAt.IsZero() || in.EndsAt.IsZero() || !in.EndsAt.After(in.StartsAt) || in.Capacity <= 0 || in.Price < 0 {
		return Session{}, errInvalidInput
	}
	var venueID string
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM venues WHERE id = ?`, in.VenueID).Scan(&venueID); errors.Is(err, sql.ErrNoRows) {
		return Session{}, errNotFound
	} else if err != nil {
		return Session{}, err
	}
	now := time.Now().UTC()
	in.Status = SessionDraft
	in.CreatedAt = now
	in.UpdatedAt = now
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions (id,venue_id,title,starts_at,ends_at,capacity,price,sold,checked_in,pending_exceptions,status,created_at,updated_at) VALUES (?,?,?,?,?,?,?,0,0,0,?,?,?)`, in.ID, in.VenueID, in.Title, in.StartsAt, in.EndsAt, in.Capacity, in.Price, in.Status, in.CreatedAt, in.UpdatedAt)
	if err != nil {
		return Session{}, fmt.Errorf("创建场次: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO session_events (session_id,action,from_status,to_status,actor,detail,created_at) VALUES (?,?,?,?,?,?,?)`, in.ID, "创建场次", "", SessionDraft, "系统", "", now)
	if err != nil {
		return Session{}, err
	}
	return in, nil
}

func (s *sqlStore) publishSession(ctx context.Context, id, actor string) (Session, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE sessions SET status = ?, updated_at = ? WHERE id = ? AND status = ?`, SessionScheduled, time.Now().UTC(), id, SessionDraft)
	if err != nil {
		return Session{}, err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		if _, e := s.getSession(ctx, id); errors.Is(e, errNotFound) {
			return Session{}, errNotFound
		}
		return Session{}, errDuplicate
	}
	now := time.Now().UTC()
	_, err = s.db.ExecContext(ctx, `INSERT INTO session_events (session_id,action,from_status,to_status,actor,detail,created_at) VALUES (?,?,?,?,?,?,?)`, id, "发布场次", SessionDraft, SessionScheduled, defaultActor(actor), "已通过排期审核", now)
	if err != nil {
		return Session{}, err
	}
	return s.getSession(ctx, id)
}

func (s *sqlStore) sellSession(ctx context.Context, id string, quantity int, actor string) (Session, []Ticket, error) {
	if quantity <= 0 {
		return Session{}, nil, errInvalidInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Session{}, nil, err
	}
	defer tx.Rollback()
	var item Session
	err = tx.QueryRowContext(ctx, `SELECT id,venue_id,title,starts_at,ends_at,capacity,price,sold,checked_in,pending_exceptions,status,created_at,updated_at FROM sessions WHERE id = ? FOR UPDATE`, id).Scan(&item.ID, &item.VenueID, &item.Title, &item.StartsAt, &item.EndsAt, &item.Capacity, &item.Price, &item.Sold, &item.CheckedIn, &item.PendingExceptions, &item.Status, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, nil, errNotFound
	}
	if err != nil {
		return Session{}, nil, err
	}
	if item.Status != SessionScheduled && item.Status != SessionSelling {
		return Session{}, nil, errInvalidTransition
	}
	if item.Sold+quantity > item.Capacity {
		return Session{}, nil, errInventoryExceeded
	}
	now := time.Now().UTC()
	if item.Status == SessionScheduled {
		if _, err = tx.ExecContext(ctx, `UPDATE sessions SET status=?,updated_at=? WHERE id=?`, SessionSelling, now, id); err != nil {
			return Session{}, nil, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO session_events (session_id,action,from_status,to_status,actor,detail,created_at) VALUES (?,?,?,?,?,?,?)`, id, "开始售票", SessionScheduled, SessionSelling, defaultActor(actor), "场次开放购票", now); err != nil {
			return Session{}, nil, err
		}
		item.Status = SessionSelling
	}
	tickets := make([]Ticket, 0, quantity)
	for i := 1; i <= quantity; i++ {
		seq := item.Sold + i
		code := fmt.Sprintf("%s-%04d", id, seq)
		ticket := Ticket{ID: fmt.Sprintf("T-%s-%04d", id, seq), SessionID: id, Code: code, Status: TicketAvailable, Price: item.Price, CreatedAt: now}
		if _, err = tx.ExecContext(ctx, `INSERT INTO tickets (id,session_id,code,status,price,created_at) VALUES (?,?,?,?,?,?)`, ticket.ID, id, code, TicketAvailable, item.Price, now); err != nil {
			return Session{}, nil, err
		}
		tickets = append(tickets, ticket)
	}
	item.Sold += quantity
	item.UpdatedAt = now
	if _, err = tx.ExecContext(ctx, `UPDATE sessions SET status=?,sold=?,updated_at=? WHERE id=?`, item.Status, item.Sold, now, id); err != nil {
		return Session{}, nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO session_events (session_id,action,from_status,to_status,actor,detail,created_at) VALUES (?,?,?,?,?,?,?)`, id, "售出门票", item.Status, item.Status, defaultActor(actor), fmt.Sprintf("本次售出 %d 张", quantity), now); err != nil {
		return Session{}, nil, err
	}
	if err = tx.Commit(); err != nil {
		return Session{}, nil, err
	}
	return item, tickets, nil
}

func (s *sqlStore) checkinSession(ctx context.Context, id, code, actor string) (Ticket, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Ticket{}, err
	}
	defer tx.Rollback()
	var ticket Ticket
	err = tx.QueryRowContext(ctx, `SELECT id,session_id,code,status,price,created_at,checked_in_at FROM tickets WHERE code = ? FOR UPDATE`, code).Scan(&ticket.ID, &ticket.SessionID, &ticket.Code, &ticket.Status, &ticket.Price, &ticket.CreatedAt, &ticket.CheckedInAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Ticket{}, errNotFound
	}
	if err != nil {
		return Ticket{}, err
	}
	if ticket.SessionID != id {
		return Ticket{}, errNotFound
	}
	if ticket.Status == TicketCheckedIn {
		return Ticket{}, errDuplicate
	}
	if ticket.Status != TicketAvailable {
		return Ticket{}, errInvalidTransition
	}
	now := time.Now().UTC()
	if _, err = tx.ExecContext(ctx, `UPDATE tickets SET status=?,checked_in_at=? WHERE code=?`, TicketCheckedIn, now, code); err != nil {
		return Ticket{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE sessions SET checked_in=checked_in+1,updated_at=? WHERE id=?`, now, id); err != nil {
		return Ticket{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO session_events (session_id,action,from_status,to_status,actor,detail,created_at) VALUES (?,?,?,?,?,?,?)`, id, "核销票码", ticket.Status, ticket.Status, defaultActor(actor), "现场检票成功", now); err != nil {
		return Ticket{}, err
	}
	if err = tx.Commit(); err != nil {
		return Ticket{}, err
	}
	ticket.Status = TicketCheckedIn
	ticket.CheckedInAt = &now
	return ticket, nil
}

func (s *sqlStore) transitionSession(ctx context.Context, id, status, actor string) (Session, error) {
	item, err := s.getSession(ctx, id)
	if err != nil {
		return Session{}, err
	}
	if !allowedSessionTransition(item.Status, status) {
		return Session{}, errInvalidTransition
	}
	now := time.Now().UTC()
	if _, err = s.db.ExecContext(ctx, `UPDATE sessions SET status=?,updated_at=? WHERE id=?`, status, now, id); err != nil {
		return Session{}, err
	}
	if _, err = s.db.ExecContext(ctx, `INSERT INTO session_events (session_id,action,from_status,to_status,actor,detail,created_at) VALUES (?,?,?,?,?,?,?)`, id, "推进状态", item.Status, status, defaultActor(actor), "运营人员确认状态", now); err != nil {
		return Session{}, err
	}
	item.Status = status
	item.UpdatedAt = now
	return item, nil
}

func (s *sqlStore) settleSession(ctx context.Context, id, actor string) (SessionSettlement, error) {
	item, err := s.getSession(ctx, id)
	if err != nil {
		return SessionSettlement{}, err
	}
	if item.Status != SessionPendingSettlement || time.Now().UTC().Before(item.EndsAt) || item.PendingExceptions > 0 {
		return SessionSettlement{}, errInvalidTransition
	}
	var existing SessionSettlement
	e := s.db.QueryRowContext(ctx, `SELECT id,session_id,ticket_count,gross,status,settled_at FROM session_settlements WHERE session_id=?`, id).Scan(&existing.ID, &existing.SessionID, &existing.TicketCount, &existing.Gross, &existing.Status, &existing.SettledAt)
	if e == nil {
		return existing, errDuplicate
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return SessionSettlement{}, e
	}
	now := time.Now().UTC()
	settlement := SessionSettlement{ID: "SET-" + id, SessionID: id, TicketCount: item.Sold, Gross: float64(item.Sold) * item.Price, Status: "已结算", SettledAt: now}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SessionSettlement{}, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE sessions SET status=?,updated_at=? WHERE id=? AND status=?`, SessionSettled, now, id, SessionPendingSettlement); err != nil {
		return SessionSettlement{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO session_settlements (id,session_id,ticket_count,gross,status,settled_at) VALUES (?,?,?,?,?,?)`, settlement.ID, id, settlement.TicketCount, settlement.Gross, settlement.Status, settlement.SettledAt); err != nil {
		return SessionSettlement{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO session_events (session_id,action,from_status,to_status,actor,detail,created_at) VALUES (?,?,?,?,?,?,?)`, id, "完成日结", SessionPendingSettlement, SessionSettled, defaultActor(actor), fmt.Sprintf("售出 %d 张，实收 %.2f", item.Sold, settlement.Gross), now); err != nil {
		return SessionSettlement{}, err
	}
	if err = tx.Commit(); err != nil {
		return SessionSettlement{}, err
	}
	return settlement, nil
}

func (s *sqlStore) listSessionEvents(ctx context.Context, id string) ([]SessionEvent, error) {
	if _, err := s.getSession(ctx, id); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,session_id,action,from_status,to_status,actor,detail,created_at FROM session_events WHERE session_id=? ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SessionEvent{}
	for rows.Next() {
		var v SessionEvent
		if err := rows.Scan(&v.ID, &v.SessionID, &v.Action, &v.FromStatus, &v.ToStatus, &v.Actor, &v.Detail, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *sqlStore) createShipment(ctx context.Context, in Shipment) (Shipment, error) {
	if in.Status == "" {
		in.Status = ShipmentPendingDispatch
	}
	if in.CreatedAt.IsZero() {
		in.CreatedAt = time.Now().UTC()
	}
	in.UpdatedAt = in.CreatedAt
	_, err := s.db.ExecContext(ctx, `INSERT INTO shipments (id, route, cargo, driver, vehicle, eta, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, in.ID, in.Route, in.Cargo, in.Driver, in.Vehicle, in.ETA, in.Status, in.CreatedAt, in.UpdatedAt)
	if err != nil {
		return Shipment{}, fmt.Errorf("创建活动订单: %w", err)
	}
	if _, err = s.db.ExecContext(ctx, `INSERT INTO shipment_events (shipment_id, from_status, to_status, actor, note, created_at) VALUES (?, '', ?, '系统', '创建活动订单', ?)`, in.ID, in.Status, in.CreatedAt); err != nil {
		return Shipment{}, fmt.Errorf("记录活动订单事件: %w", err)
	}
	return in, nil
}

func (s *sqlStore) getShipment(ctx context.Context, id string) (Shipment, error) {
	var v Shipment
	err := s.db.QueryRowContext(ctx, `SELECT id, route, cargo, driver, vehicle, eta, status, created_at, updated_at FROM shipments WHERE id = ?`, id).Scan(&v.ID, &v.Route, &v.Cargo, &v.Driver, &v.Vehicle, &v.ETA, &v.Status, &v.CreatedAt, &v.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Shipment{}, errNotFound
	}
	if err != nil {
		return Shipment{}, fmt.Errorf("查询活动订单: %w", err)
	}
	return v, nil
}

func (s *sqlStore) listShipments(ctx context.Context, status string, page, pageSize int) ([]Shipment, int) {
	var total int
	countQuery := `SELECT COUNT(*) FROM shipments`
	args := []any{}
	if status != "" {
		countQuery += ` WHERE status = ?`
		args = append(args, status)
	}
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return []Shipment{}, 0
	}
	query := `SELECT id, route, cargo, driver, vehicle, eta, status, created_at, updated_at FROM shipments`
	args = []any{}
	if status != "" {
		query += ` WHERE status = ?`
		args = append(args, status)
	}
	query += ` ORDER BY updated_at DESC LIMIT ? OFFSET ?`
	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return []Shipment{}, total
	}
	defer rows.Close()
	out := []Shipment{}
	for rows.Next() {
		var v Shipment
		if err := rows.Scan(&v.ID, &v.Route, &v.Cargo, &v.Driver, &v.Vehicle, &v.ETA, &v.Status, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return []Shipment{}, total
		}
		out = append(out, v)
	}
	return out, total
}

func (s *sqlStore) assignShipment(ctx context.Context, id, driver, actor string) (Shipment, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Shipment{}, err
	}
	defer tx.Rollback()
	var current string
	if err = tx.QueryRowContext(ctx, `SELECT status FROM shipments WHERE id = ? FOR UPDATE`, id).Scan(&current); errors.Is(err, sql.ErrNoRows) {
		return Shipment{}, errNotFound
	} else if err != nil {
		return Shipment{}, err
	}
	if current != ShipmentPendingDispatch && current != ShipmentPendingAccept {
		return Shipment{}, errInvalidTransition
	}
	now := time.Now().UTC()
	if _, err = tx.ExecContext(ctx, `UPDATE shipments SET driver = ?, status = ?, updated_at = ? WHERE id = ?`, driver, ShipmentPendingAccept, now, id); err != nil {
		return Shipment{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO shipment_events (shipment_id, from_status, to_status, actor, note, created_at) VALUES (?, ?, ?, ?, ?, ?)`, id, current, ShipmentPendingAccept, actor, "已分配场馆工作人员", now); err != nil {
		return Shipment{}, err
	}
	if err = tx.Commit(); err != nil {
		return Shipment{}, err
	}
	return s.getShipment(ctx, id)
}

func (s *sqlStore) transitionShipment(ctx context.Context, id, status, actor, note string) (Shipment, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Shipment{}, err
	}
	defer tx.Rollback()
	var current string
	if err = tx.QueryRowContext(ctx, `SELECT status FROM shipments WHERE id = ? FOR UPDATE`, id).Scan(&current); errors.Is(err, sql.ErrNoRows) {
		return Shipment{}, errNotFound
	} else if err != nil {
		return Shipment{}, err
	}
	if !allowedShipmentTransition(current, status) {
		return Shipment{}, errInvalidTransition
	}
	now := time.Now().UTC()
	if _, err = tx.ExecContext(ctx, `UPDATE shipments SET status = ?, updated_at = ? WHERE id = ?`, status, now, id); err != nil {
		return Shipment{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO shipment_events (shipment_id, from_status, to_status, actor, note, created_at) VALUES (?, ?, ?, ?, ?, ?)`, id, current, status, actor, note, now); err != nil {
		return Shipment{}, err
	}
	if err = tx.Commit(); err != nil {
		return Shipment{}, err
	}
	return s.getShipment(ctx, id)
}

func (s *sqlStore) listShipmentEvents(ctx context.Context, id string) ([]ShipmentEvent, error) {
	if _, err := s.getShipment(ctx, id); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, shipment_id, from_status, to_status, actor, note, created_at FROM shipment_events WHERE shipment_id = ? ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ShipmentEvent{}
	for rows.Next() {
		var v ShipmentEvent
		if err := rows.Scan(&v.ID, &v.ShipmentID, &v.FromStatus, &v.ToStatus, &v.Actor, &v.Note, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *sqlStore) listDrivers(ctx context.Context) []Driver {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, phone, vehicle, status FROM drivers ORDER BY id`)
	if err != nil {
		return []Driver{}
	}
	defer rows.Close()
	out := []Driver{}
	for rows.Next() {
		var v Driver
		if rows.Scan(&v.ID, &v.Name, &v.Phone, &v.Vehicle, &v.Status) == nil {
			out = append(out, v)
		}
	}
	return out
}
func (s *sqlStore) listVehicles(ctx context.Context) []Vehicle {
	rows, err := s.db.QueryContext(ctx, `SELECT id, plate, type, status FROM vehicles ORDER BY id`)
	if err != nil {
		return []Vehicle{}
	}
	defer rows.Close()
	out := []Vehicle{}
	for rows.Next() {
		var v Vehicle
		if rows.Scan(&v.ID, &v.Plate, &v.Type, &v.Status) == nil {
			out = append(out, v)
		}
	}
	return out
}
func (s *sqlStore) listExceptions(ctx context.Context, status string) []Exception {
	query := `SELECT id, shipment_id, type, text, level, status, created_at, resolved_at FROM exceptions`
	args := []any{}
	if status != "" {
		query += ` WHERE status = ?`
		args = append(args, status)
	}
	query += ` ORDER BY created_at DESC`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return []Exception{}
	}
	defer rows.Close()
	out := []Exception{}
	for rows.Next() {
		var v Exception
		if rows.Scan(&v.ID, &v.ShipmentID, &v.Type, &v.Text, &v.Level, &v.Status, &v.CreatedAt, &v.ResolvedAt) == nil {
			out = append(out, v)
		}
	}
	return out
}
func (s *sqlStore) resolveException(ctx context.Context, id string) (Exception, error) {
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `UPDATE exceptions SET status = '已处理', resolved_at = ? WHERE id = ?`, now, id)
	if err != nil {
		return Exception{}, err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return Exception{}, errNotFound
	}
	for _, v := range s.listExceptions(ctx, "") {
		if v.ID == id {
			return v, nil
		}
	}
	return Exception{}, errNotFound
}
func (s *sqlStore) listSettlements(ctx context.Context) []Settlement {
	rows, err := s.db.QueryContext(ctx, `SELECT id, period, status, driver_count, shipment_count, amount FROM settlements ORDER BY id`)
	if err != nil {
		return []Settlement{}
	}
	defer rows.Close()
	out := []Settlement{}
	for rows.Next() {
		var v Settlement
		if rows.Scan(&v.ID, &v.Period, &v.Status, &v.DriverCount, &v.ShipmentCount, &v.Amount) == nil {
			out = append(out, v)
		}
	}
	return out
}
func (s *sqlStore) confirmSettlement(ctx context.Context, id string) (Settlement, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE settlements SET status = '已结算' WHERE id = ?`, id)
	if err != nil {
		return Settlement{}, err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return Settlement{}, errNotFound
	}
	for _, v := range s.listSettlements(ctx) {
		if v.ID == id {
			return v, nil
		}
	}
	return Settlement{}, errNotFound
}
