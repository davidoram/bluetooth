package main

/*
 * Peripheral is the server component
 * Accpets HPS requests & calls out to local service
 *
 */

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	golog "log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/davidoram/bluetooth/hps"
	"github.com/paypal/gatt"
	"github.com/paypal/gatt/examples/option"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var (
	deviceName *string
	level      *string
	consoleLog *bool
)

func init() {
	golog.SetOutput(ioutil.Discard)
	// UNIX Time is faster and smaller than most timestamps
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	// id = flag.String("id", hps.PeripheralID, "Peripheral ID")
	deviceName = flag.String("name", hps.DeviceName, "Device name to advertise")
	level = flag.String("level", "info", "Logging level, eg: panic, fatal, error, warn, info, debug, trace")
	consoleLog = flag.Bool("console-log", true, "Pass true to enable colorized console logging, false for JSON style logging")
}

func onStateChanged(device gatt.Device, s gatt.State) {
	log.Info().
		Str("State", s.String()).
		Msg("State changed")
	switch s {
	case gatt.StatePoweredOn:
		log.Info().
			Msg("Start scanning")
		device.Scan([]gatt.UUID{}, true)
		return
	default:
		log.Info().
			Msg("Stop scanning")
		device.StopScanning()
	}
}

type savedRequest struct {
	URI     string
	Headers string
	Body    []byte
	Method  string
	Scheme  string
}

var (
	request  *savedRequest
	response *hps.Response
)

func sendRequest(r savedRequest) error {

	response = nil

	// Create client
	client := &http.Client{}

	// Create request
	req, err := http.NewRequest(r.Method, fmt.Sprintf("%s://%s", r.Scheme, r.URI), bytes.NewReader(r.Body))

	// Headers
	if r.Headers != "" {
		for _, h := range strings.Split(r.Headers, "\n") {
			values := strings.Split(h, "=")
			if len(values) != 2 {
				log.Warn().Str("header", h).Msg("Ingoring invalid header")
				continue
			}
			req.Header.Add(values[0], values[1])
		}
	}

	// Fetch Request
	log.Info().Msg("Proxying request")
	resp, err := client.Do(req)

	if err != nil {
		log.Err(err).Msg("HTTP call failed")
		return err
	}

	// Read Response Body
	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Err(err).Msg("Ready response body failed")
		return err
	}

	b, trunc := hps.EncodeHeaders(resp.Header)
	response = &hps.Response{
		NotifyStatus: hps.NotifyStatus{
			StatusCode:       resp.StatusCode,
			HeadersReceived:  true,
			HeadersTruncated: trunc,
			BodyReceived:     len(respBody) > 0,
			BodyTruncated:    len(respBody) > hps.BodyMaxOctets,
		},
		Headers: b,
		Body:    respBody,
	}
	return nil
}

func NewHPSService() *gatt.Service {
	s := gatt.NewService(gatt.MustParseUUID(hps.HpsServiceID))

	// URI
	s.AddCharacteristic(gatt.UUID16(hps.HTTPURIID)).HandleWriteFunc(
		func(r gatt.Request, data []byte) (status byte) {
			request.URI = string(data)
			log.Debug().Str("attr", "URI").Str("val", request.URI).Str("op", "write")
			return gatt.StatusSuccess
		})

	// Headers
	hc := s.AddCharacteristic(gatt.UUID16(hps.HTTPHeadersID))
	hc.HandleWriteFunc(
		func(r gatt.Request, data []byte) (status byte) {
			request.Headers = string(data)
			log.Debug().Str("attr", "headers").Str("val", request.Headers).Str("op", "write")
			return gatt.StatusSuccess
		})
	hc.HandleReadFunc(
		func(rsp gatt.ResponseWriter, req *gatt.ReadRequest) {
			if response != nil {
				log.Debug().Str("attr", "headers").Str("val", request.Headers).Str("op", "read")
				_, err := rsp.Write(response.Headers)
				if err != nil {
					log.Err(err).Str("attr", "headers").Str("val", request.Headers).Str("op", "read")
				}
			} else {
				log.Warn().Str("attr", "headers").Str("op", "read").Msg("Read received before response has arrived")
			}
		})

	// Body
	hb := s.AddCharacteristic(gatt.UUID16(hps.HTTPEntityBodyID))
	hb.HandleWriteFunc(
		func(r gatt.Request, data []byte) (status byte) {
			request.Body = data
			log.Debug().Str("attr", "body").Interface("val", string(request.Body)).Str("op", "write")
			return gatt.StatusSuccess
		})
	hb.HandleReadFunc(
		func(rsp gatt.ResponseWriter, req *gatt.ReadRequest) {
			if response != nil {
				log.Debug().Str("attr", "body").Interface("val", string(response.Body)).Str("op", "read")
				_, err := rsp.Write(response.Body)
				if err != nil {
					log.Err(err).Str("attr", "body").Interface("val", string(response.Body)).Str("op", "read")
				}
			} else {
				log.Warn().Str("attr", "body").Str("op", "read").Msg("Read received before response has arrived")
			}
		})

	// Receive control point, this triggers the HTTP request to occur
	s.AddCharacteristic(gatt.UUID16(hps.HTTPControlPointID)).HandleWriteFunc(
		func(r gatt.Request, data []byte) (status byte) {
			var err error
			request.Method, err = hps.DecodeHttpMethod(data[0])
			if err != nil {
				log.Err(err).Str("attr", "control").Str("sub_attr", "Method").Str("op", "write")
				return gatt.StatusUnexpectedError // TODO is this correct?
			}
			log.Debug().Str("attr", "control").Str("sub_attr", "Method").Str("val", request.Method).Str("op", "write")

			request.Scheme, err = hps.DecodeURLScheme(data[0])
			if err != nil {
				log.Err(err).Str("attr", "control").Str("sub_attr", "Scheme").Str("op", "write")
				return gatt.StatusUnexpectedError // TODO is this correct?
			}
			log.Debug().Str("attr", "control").Str("sub_attr", "Scheme").Str("val", request.Scheme).Str("op", "write")

			// Make the API call in the background
			go sendRequest(*request)

			// Reset inputs, ready for the next call
			request = &savedRequest{}

			return gatt.StatusSuccess
		})

	s.AddCharacteristic(gatt.UUID16(hps.HTTPStatusCodeID)).HandleNotifyFunc(
		func(r gatt.Request, n gatt.Notifier) {
			for !n.Done() {
				if response != nil && !response.Notified {
					log.Info().Str("attr", "status_code").Int("val", response.NotifyStatus.StatusCode).Str("op", "notify").Msg("notify response")
					_, err := n.Write(response.NotifyStatus.Encode())
					if err != nil {
						log.Err(err).Str("attr", "status_code").Int("val", response.NotifyStatus.StatusCode).Str("op", "notify")
					}
					response.Notified = true
				} else {
					time.Sleep(time.Millisecond * 100)
				}
			}
		})

	return s
}

var (
	poweredOn bool
)

func main() {

	flag.Parse()

	if *consoleLog {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	}
	lvl, err := zerolog.ParseLevel(*level)
	if err != nil {
		lvl = zerolog.DebugLevel
	}
	zerolog.SetGlobalLevel(lvl)
	if err != nil {
		log.Panic().Str("level", *level).Msg("Invalid log level")
	}

	log.Info().Str("device_name", *deviceName).Msg("creating")

	d, err := gatt.NewDevice(option.DefaultServerOptions...)
	if err != nil {
		log.Err(err)
	}

	// Init space for the next request
	request = &savedRequest{}

	// Register optional handlers.
	d.Handle(
		gatt.CentralConnected(func(c gatt.Central) {
			log.Info().Str("central_id", c.ID()).Msg("Central connected")
		}),
		gatt.CentralDisconnected(func(c gatt.Central) {
			log.Info().Str("central_id", c.ID()).Msg("Central disconnected")
		}),
	)

	// A mandatory handler for monitoring device state.
	onStateChanged := func(d gatt.Device, s gatt.State) {
		log.Info().Str("state", s.String()).Msg("State changed")
		switch s {
		case gatt.StatePoweredOn:
			poweredOn = true
			s1 := NewHPSService()
			d.AddService(s1)
			go advertisePeriodically(d, hps.DeviceName, []gatt.UUID{s1.UUID()})

		default:
			poweredOn = false
		}
	}

	d.Init(onStateChanged)
	select {}
}

func advertisePeriodically(d gatt.Device, deviceName string, services []gatt.UUID) {
	log.Info().Msg("Start advertising")
	for poweredOn {
		// Advertise device name and service's UUIDs.
		d.AdvertiseNameAndServices(hps.DeviceName, services)
		time.Sleep(time.Millisecond * 100)
	}
	log.Info().Msg("Stop advertising")
}
