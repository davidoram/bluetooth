package main

/*
 * Central is the client component
 * Takes command line options and translates then to bluetooth calls to the server
 *
 */

import (
	"time"

	"github.com/davidoram/bluetooth/hps"
	"github.com/paypal/gatt"
	"github.com/rs/zerolog/log"
)

type Connection struct {
	Request  hps.Request
	Response hps.Response
	Error    error

	Timeout time.Duration

	// Will be sent a true (we got a response) or false (no response)
	ResponseChannel chan bool
}

func MakeConnection() Connection {
	return Connection{ResponseChannel: make(chan bool, 1)}
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
			f := func(c *gatt.Characteristic, b []byte, err error) {
				log.Info().Str("name", c.Name()).Bytes("value", b).Msg("notified")
				if c.UUID().Equal(gatt.UUID16(hps.HTTPStatusCodeID)) {
					ns, err := hps.DecodeNotifyStatus(b)
					if err != nil {
						log.Err(err).Msg("decode notify status")
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
				}
			}
			if err := p.SetNotifyValue(c, f); err != nil {
				log.Err(err).Msg("subscribe to notifications")
				continue
			}
		}

	}
	return nil
}
