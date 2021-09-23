package main

/*
 * Central is the client component
 * Takes command line options and translates then to bluetooth calls to the server
 *
 */

import (
	"context"
	"fmt"
	"time"

	"github.com/davidoram/bluetooth/hps"
	"github.com/paypal/gatt"
	"github.com/paypal/gatt/examples/option"
	"github.com/rs/zerolog/log"
)

type Connection struct {
	Request  hps.Request
	Response hps.Response
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

func (conn *Connection) Connect() (hps.Response, error) {
	resp := hps.Response{}
	d, err := gatt.NewDevice(option.DefaultClientOptions...)
	if err != nil {
		return resp, err
	}
	defer d.StopScanning()

	// Register handlers.
	d.Handle(
		gatt.PeripheralDiscovered(conn.onPeriphDiscovered),
		gatt.PeripheralConnected(conn.onPeriphConnected),
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
	if p.Name() != *deviceName {
		log.Debug().Str("peripheral_id", p.ID()).Str("name", p.Name()).Msg("Skipping")
		return
	}

	// Stop scanning once we've got the peripheral we're looking for.
	log.Info().Str("peripheral_id", p.ID()).Str("name", p.Name()).Msg("Found peripheral")
	log.Info().Msg("stop scanning")
	p.Device().StopScanning()

	log.Debug().Str("local_name", a.LocalName).
		Int("tx_power_level", a.TxPowerLevel).
		Bytes("manufacturer_data", a.ManufacturerData).
		Interface("service_data", a.ServiceData).Msg("scan")

	log.Info().Msg("connect")
	p.Device().Connect(p)
}

func (c *Connection) onPeriphConnected(p gatt.Peripheral, err error) {
	log.Info().Msg("connected")

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
		if s.UUID().Equal(gatt.MustParseUUID(hps.HpsServiceID)) {
			hpsService = s
			err := c.parseService(p)
			if err != nil {
				log.Err(err).Msg("Discover services")
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

func (conn *Connection) parseService(p gatt.Peripheral) error {
	log.Debug().Msg("parse service")

	// Discovery characteristics
	cs, err := p.DiscoverCharacteristics(nil, hpsService)
	if err != nil {
		return err
	}
	for _, c := range cs {
		log.Debug().Str("name", c.Name()).Msg("characteristic")
		switch c.UUID().String() {
		case gatt.UUID16(hps.HTTPURIID).String():
			uriChr = c
		case gatt.UUID16(hps.HTTPHeadersID).String():
			hdrsChr = c
		case gatt.UUID16(hps.HTTPEntityBodyID).String():
			bodyChr = c
		case gatt.UUID16(hps.HTTPControlPointID).String():
			controlChr = c
		case gatt.UUID16(hps.HTTPStatusCodeID).String():
			statusChr = c
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
	if c.UUID().Equal(gatt.UUID16(hps.HTTPStatusCodeID)) {
		var ns hps.NotifyStatus
		ns, conn.Error = hps.DecodeNotifyStatus(b)
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
		conn.Response = hps.Response{NotifyStatus: ns}
		conn.ResponseChannel <- true
	} else {
		log.Warn().Msg("Unknown characteristic on notification")
		conn.Error = fmt.Errorf("Notified on unknown charachteristic")
	}
}
