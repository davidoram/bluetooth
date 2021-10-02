package hps

/*
 * Create Client to take control over the HTTP headers, timeouts, and retry policies
 */

import (
	"context"
	"fmt"
	"time"

	"github.com/paypal/gatt"
	"github.com/paypal/gatt/examples/option"
	"github.com/rs/zerolog/log"
)

type Connection struct {
	DeviceName string

	hpsService               *gatt.Service
	uriChr, hdrsChr, bodyChr *gatt.Characteristic
	controlChr, statusChr    *gatt.Characteristic

	Request  Request
	Response Response
	Error    error

	ctx    context.Context
	cancel context.CancelFunc

	// Will be sent a true (we got a response) or false (no response)
	ResponseChannel chan bool
}

func MakeConnection(timeout time.Duration) Connection {
	// Create a new context
	ctx := context.Background()
	// Create a new context, with its cancellation function
	// from the original context
	ctx, cancel := context.WithTimeout(ctx, timeout)

	return Connection{
		ResponseChannel: make(chan bool, 1),
		ctx:             ctx,
		cancel:          cancel,
	}

}

func (conn *Connection) Connect() (Response, error) {
	resp := Response{}
	d, err := gatt.NewDevice(option.DefaultClientOptions...)
	if err != nil {
		return resp, err
	}
	defer d.StopScanning()

	// Register handlers.
	d.Handle(
		gatt.PeripheralDiscovered(conn.onPeriphDiscovered),
		gatt.PeripheralConnected(conn.onPeriphConnected),
		gatt.PeripheralDisconnected(conn.onPeriphDisconnected),
	)

	d.Init(conn.onStateChanged)
	// Wait for either the ConnectionTimeout to expire, or to be connected
	select {
	case <-conn.ResponseChannel:
		log.Info().Msg("Got response")
		return resp, nil
	case <-conn.ctx.Done():
		// Stop waiting for response
		conn.ResponseChannel <- false

		log.Info().Msg("Got Timeouts")
		return resp, fmt.Errorf("Timeout")
	}
}

func (c *Connection) onStateChanged(d gatt.Device, s gatt.State) {
	log.Info().Str("state", s.String()).Msg("state changed")
	switch s {
	case gatt.StatePoweredOn:
		c.scanAll(d)
	default:
		log.Info().Msg("stop scanning")
		d.StopScanning()
	}
}

func (c *Connection) scanAll(d gatt.Device) {
	log.Info().Msg("scan all devices")
	d.Scan([]gatt.UUID{}, false)
	log.Info().Msg("finished scan")
}

func (c *Connection) onPeriphDiscovered(p gatt.Peripheral, a *gatt.Advertisement, rssi int) {
	if p.Name() != c.DeviceName {
		log.Debug().Str("peripheral_id", p.ID()).Str("name", p.Name()).Msg("Skipping")
		return
	}

	// Stop scanning once we've got the peripheral we're looking for.
	log.Info().Str("peripheral_id", p.ID()).Str("name", p.Name()).Msg("Found HPS server")
	p.Device().StopScanning()
	p.Device().Connect(p)
}

func (c *Connection) onPeriphConnected(p gatt.Peripheral, err error) {
	log.Info().Msg("Peripheral connected")

	if err := p.SetMTU(500); err != nil {
		log.Err(err).Msg("MTU set")
	}

	// Discovery services
	ss, err := p.DiscoverServices(nil)
	if err != nil {
		log.Err(err).Msg("Discover services")
		return
	}

	for _, s := range ss {
		if s.UUID().Equal(gatt.MustParseUUID(HpsServiceID)) {
			c.hpsService = s
			err := c.parseService(p)
			if err != nil {
				log.Err(err).Msg("Parse services")
				continue
			}
			c.Error = c.CallService(p)
			if c.Error != nil {
				log.Err(err).Msg("call service")
			}
			break
		}
	}
}

func (c *Connection) onPeriphDisconnected(p gatt.Peripheral, err error) {
	log.Info().Msg("Peripheral disconnected")
	c.cancel()
}

func (conn *Connection) parseService(p gatt.Peripheral) error {
	log.Debug().Msg("parse service")

	// Discovery characteristics
	cs, err := p.DiscoverCharacteristics(nil, conn.hpsService)
	if err != nil {
		return err
	}
	for _, c := range cs {
		log.Debug().Str("name", c.Name()).Msg("characteristic")
		switch c.UUID().String() {
		case gatt.UUID16(HTTPURIID).String():
			conn.uriChr = c
		case gatt.UUID16(HTTPHeadersID).String():
			conn.hdrsChr = c
		case gatt.UUID16(HTTPEntityBodyID).String():
			conn.bodyChr = c
		case gatt.UUID16(HTTPControlPointID).String():
			conn.controlChr = c
		case gatt.UUID16(HTTPStatusCodeID).String():
			conn.statusChr = c
		}

		// Discovery descriptors
		ds, err := p.DiscoverDescriptors(nil, c)
		if err != nil {
			log.Err(err).Msg("discover descriptors")
			continue
		}

		for _, d := range ds {
			// Read descriptor (could fail, if it's not readable)
			b, err := p.ReadDescriptor(d)
			if err != nil {
				log.Err(err).Str("name", d.Name()).Msg("read descriptor")
				continue
			}
			log.Debug().Str("name", d.Name()).Bytes("value", b).Msg("readdescriptor")
		}

		// Subscribe the characteristic, if possible.
		if (c.Properties() & (gatt.CharNotify | gatt.CharIndicate)) != 0 {

			if err := p.SetNotifyValue(c, conn.onCallback); err != nil {
				log.Err(err).Msg("subscribe to notifications")
				continue
			}
		}
	}
	return nil
}

func (conn *Connection) onCallback(c *gatt.Characteristic, b []byte, err error) {
	if err != nil {
		log.Err(err).Msg("notified")
		conn.Error = err
		return
	}
	log.Info().Str("name", c.Name()).Bytes("value", b).Msg("notified")
	if c.UUID().Equal(gatt.UUID16(HTTPStatusCodeID)) {
		var ns NotifyStatus
		ns, conn.Error = DecodeNotifyStatus(b)
		if conn.Error != nil {
			log.Err(conn.Error).Msg("decode notify status")
			return
		}
		log.Info().Int("http_status", ns.StatusCode).
			Bool("headers_received", ns.HeadersReceived).
			Bool("headers_truncated", ns.HeadersTruncated).
			Bool("body_received", ns.BodyReceived).
			Bool("body_truncated", ns.BodyTruncated).
			Msg("decoded notify status")
		conn.Response = Response{NotifyStatus: ns}
		conn.ResponseChannel <- true
	} else {
		log.Warn().Msg("Unknown characteristic on notification")
		conn.Error = fmt.Errorf("Notified on unknown charachteristic")
	}
}
