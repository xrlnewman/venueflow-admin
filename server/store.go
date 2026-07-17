package main

import "context"

type fleetStore interface {
	listVenues(context.Context) []Venue
	listSessions(context.Context, string, int, int) ([]Session, int)
	getSession(context.Context, string) (Session, error)
	createSession(context.Context, Session) (Session, error)
	publishSession(context.Context, string, string) (Session, error)
	sellSession(context.Context, string, int, string) (Session, []Ticket, error)
	checkinSession(context.Context, string, string, string) (Ticket, error)
	transitionSession(context.Context, string, string, string) (Session, error)
	settleSession(context.Context, string, string) (SessionSettlement, error)
	listSessionEvents(context.Context, string) ([]SessionEvent, error)
	createShipment(context.Context, Shipment) (Shipment, error)
	getShipment(context.Context, string) (Shipment, error)
	listShipments(context.Context, string, int, int) ([]Shipment, int)
	assignShipment(context.Context, string, string, string) (Shipment, error)
	transitionShipment(context.Context, string, string, string, string) (Shipment, error)
	listShipmentEvents(context.Context, string) ([]ShipmentEvent, error)
	listDrivers(context.Context) []Driver
	listVehicles(context.Context) []Vehicle
	listExceptions(context.Context, string) []Exception
	resolveException(context.Context, string) (Exception, error)
	listSettlements(context.Context) []Settlement
	confirmSettlement(context.Context, string) (Settlement, error)
}
