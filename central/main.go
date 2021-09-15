package main

/*
 * Central is the client component
 * Takes command line options and translates then to bluetooth calls to the server
 *
 */

import (
	"flag"
	"fmt"
	"io/ioutil"
	golog "log"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/davidoram/bluetooth/hps"
	"github.com/paypal/gatt"
	"github.com/paypal/gatt/examples/option"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Define a new type that can accept multiple values passed on the command line
type arrayStr []string

func (i *arrayStr) String() string {
	return strings.Join([]string(*i), "\n")
}

func (i *arrayStr) Set(value string) error {
	if len(strings.Split(value, "=")) != 2 {
		return fmt.Errorf("Invalid format, expect 'key=value'")
	}
	*i = append(*i, value)
	return nil
}

var (
	// id          *string
	deviceName *string

	uri     *string
	u       *url.URL
	headers arrayStr
	body    *string
	method  *string
	output  *string

	level      *string
	consoleLog *bool

	responseTimeout *time.Duration

	responseChannel = make(chan bool, 1)
	response        *hps.Response

	done = make(chan struct{})
)

func init() {
	golog.SetOutput(ioutil.Discard)
	// UNIX Time is faster and smaller than most timestamps
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix

	// id = flag.String("id", hps.PeripheralID, "Peripheral ID to scan for")
	deviceName = flag.String("name", hps.DeviceName, "Device name to scan for")
	uri = flag.String("url", "", "Specify a URL to fetch eg: --url http://localhost:8100/hello.txt")
	flag.Var(&headers, "header", `Specify an HTTP header. This flag can be repeated to send multiple headers. eg: -header "Accept=text/plain"`)
	body = flag.String("data", "", "Send the specified data in a POST, PUT or PATCH request")
	method = flag.String("request", "GET", "Specify the request method which can beone of: GET, PUT, POST, PATCH, or DELETE")
	output = flag.String("output", "", "Write output to <file> instead of stdout. Will overwrite file if it exists.")
	responseTimeout = flag.Duration("timeout", time.Second*30, "Time to wait for server to return response")
	level = flag.String("level", "info", "Logging level, eg: panic, fatal, error, warn, info, debug, trace")
	consoleLog = flag.Bool("console-log", true, "Pass true to enable colorized console logging, false for JSON style logging")

}

func onStateChanged(d gatt.Device, s gatt.State) {
	log.Info().Str("state", s.String()).Msg("state changed")
	switch s {
	case gatt.StatePoweredOn:
		go scanPeriodically(d)
	default:
		log.Info().Msg("stop scanning")
		d.StopScanning()
	}
}

var (
	foundServer bool
)

func scanPeriodically(d gatt.Device) {
	log.Info().Msg("start periodic scan")
	for !foundServer {
		d.Scan([]gatt.UUID{}, false)
		time.Sleep(time.Millisecond * 100)
	}
	log.Info().Msg("stop periodic scan")
}

func onPeriphDiscovered(p gatt.Peripheral, a *gatt.Advertisement, rssi int) {
	if p.Name() != *deviceName {
		log.Debug().Str("peripheral_id", p.ID()).Str("name", p.Name()).Msg("Skipping")
		return
	}
	foundServer = true

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

var (
	hpsService *gatt.Service
)

func onPeriphConnected(p gatt.Peripheral, err error) {
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
			err := parseService(p)
			if err != nil {
				log.Err(err).Msg("Discover services")
				continue
			}
			err = callService(p)
			if err != nil {
				log.Err(err).Msg("call service")
			}
			break
		}
	}
}

var (
	uriChr, hdrsChr, bodyChr, controlChr, statusChr *gatt.Characteristic
)

func parseService(p gatt.Peripheral) error {
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

		// // Read the characteristic, if possible.
		// if (c.Properties() & gatt.CharRead) != 0 {
		// 	log.Debug(err).Str("name", c.Name()).Msg("failed to read")
		// 	b, err := p.ReadCharacteristic(c)
		// 	if err != nil {
		// 		log.Err(err).Str("name", c.Name()).Msg("failed to read")
		// 		continue
		// 	}
		// 	log.Err(err).Bytes("value",b).Msg("read")
		// }

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
					response = &hps.Response{NotifyStatus: ns}
					responseChannel <- true
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

func check(err error, msg string) {
	if err != nil {
		log.Fatal().Err(err).Msg(msg)
	}
}

func callService(p gatt.Peripheral) error {
	defer p.Device().CancelConnection(p)

	log.Info().Str("uri", u.String()).
		Interface("headers", headers).
		Str("body", *body).
		Str("method", *method).
		Str("schema", u.Scheme).
		Msg("call service")

	urlStr := fmt.Sprintf("%s%s", u.Host, u.EscapedPath())
	err := p.WriteCharacteristic(uriChr, []byte(urlStr), true)
	if err != nil {
		return err
	}

	err = p.WriteCharacteristic(hdrsChr, []byte(headers.String()), true)
	if err != nil {
		return err
	}

	err = p.WriteCharacteristic(bodyChr, []byte(*body), true)
	if err != nil {
		return err
	}

	code, err := hps.EncodeMethodScheme(*method, u.Scheme)
	if err != nil {
		return err
	}
	err = p.WriteCharacteristic(controlChr, []byte{code}, false)
	if err != nil {
		return err
	}

	log.Info().Dur("timeout", *responseTimeout).Msg("waiting for notification")
	time.AfterFunc(*responseTimeout, func() {
		log.Warn().Msg("timeout expired, no notification received")
		responseChannel <- false
	})
	gotResponse := <-responseChannel
	if gotResponse {
		response.Body, err = p.ReadCharacteristic(bodyChr)
		if err != nil {
			return err
		}
		// Write to stdout or file
		if *output == "" {
			fmt.Print(string(response.Body))
		} else {
			f, err := os.Create(*output)
			check(err, fmt.Sprintf("Error opening file: '%s'", *output))
			defer f.Close()
			f.WriteString(string(response.Body))
		}

		response.Headers, err = p.ReadCharacteristic(hdrsChr)
		if err != nil {
			return err
		}
		log.Info().Str("body", string(response.Body)).
			Interface("headers", response.DecodedHeaders()).
			Bool("headers_received", response.NotifyStatus.HeadersReceived).
			Bool("headers_truncated", response.NotifyStatus.HeadersTruncated).
			Bool("body_received", response.NotifyStatus.BodyReceived).
			Bool("body_truncated", response.NotifyStatus.BodyTruncated).
			Msg("read resoponse")
	}

	return nil
}

func onPeriphDisconnected(p gatt.Peripheral, err error) {
	log.Info().Msg("disconnected")
	close(done)
}

func main() {
	flag.Parse()
	if *consoleLog {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339})
	}
	lvl, err := zerolog.ParseLevel(*level)
	if err != nil {
		lvl = zerolog.DebugLevel
	}
	zerolog.SetGlobalLevel(lvl)
	if err != nil {
		log.Panic().Str("level", *level).Msg("Invalid log level")
	}
	log.Info().Str("level", lvl.String()).Msg("Log level set")

	u, err = url.Parse(*uri)
	if err != nil {
		log.Err(err).Msg("Parse error")
		return
	}
	log.Info().Str("device_name", *deviceName).Msg("starting up")

	d, err := gatt.NewDevice(option.DefaultClientOptions...)
	if err != nil {
		log.Err(err).Msg("Device failed")
		return
	}

	// Register handlers.
	d.Handle(
		gatt.PeripheralDiscovered(onPeriphDiscovered),
		gatt.PeripheralConnected(onPeriphConnected),
		gatt.PeripheralDisconnected(onPeriphDisconnected),
	)

	d.Init(onStateChanged)
	<-done
	log.Info().Msg("Done")
}
