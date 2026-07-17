package main

import "context"

type fleetStore interface {
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
