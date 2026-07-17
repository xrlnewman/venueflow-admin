package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type sqlStore struct{ db *sql.DB }

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
