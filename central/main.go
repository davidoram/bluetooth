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
	"time"

	"github.com/davidoram/bluetooth/hps"
	"github.com/paypal/gatt"
	"github.com/paypal/gatt/examples/option"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var (
	// id          *string
	deviceName *string

	uri     *string
	u       *url.URL
	headers hps.ArrayStr
	body    *string
	method  *string
	output  *string

	level      *string
	consoleLog *bool

	responseTimeout *time.Duration
	response        hps.Response

	done = make(chan struct{})
)

func init() {
	golog.SetOutput(ioutil.Discard)
	// UNIX Time is faster and smaller than most timestamps
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	// id = flag.String("id", hps.PeripheralID, "Peripheral ID to scan for")
	deviceName = flag.String("name", hps.DeviceName, "Device name to scan for")
	uri = flag.String("url", "", "Specify a URL to fetch eg: --url http://localhost:8100/hello.txt")
	flag.Var(&headers, "header", `Specify an HTTP header. This flag can be repeated to send multiple headers. eg: -header "Accept=text/plain"`)
	body = flag.String("data", "", "Send the specified data in a POST, PUT or PATCH request")
	method = flag.String("request", "GET", "Specify the request method which can beone of: GET, PUT, POST, PATCH, or DELETE")
	output = flag.String("output", "", "Write output to <file> instead of stdout. Will overwrite file if it exists.")
	responseTimeout = flag.Duration("timeout", time.Second*30, "Time to wait for server to return response")
	level = flag.String("log-level", "info", "Logging level, eg: panic, fatal, error, warn, info, debug, trace")
	consoleLog = flag.Bool("log-console", false, "Pass true to enable colorized console logging, false for JSON style logging")
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
	var sleepScan = time.Millisecond * 300
	// Only display a message every minute
	sampled := log.Sample(&zerolog.BasicSampler{N: 50})

	for !foundServer {
		d.Scan([]gatt.UUID{}, false)
		time.Sleep(sleepScan)
		sampled.Debug().Msg("scanning")
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

var (
	uriChr, hdrsChr, bodyChr, controlChr, statusChr *gatt.Characteristic
)

func check(err error, msg string) {
	if err != nil {
		log.Fatal().Err(err).Msg(msg)
	}
}

func onPeriphDisconnected(p gatt.Peripheral, err error) {
	log.Info().Msg("disconnected")
	close(done)
}

func main() {
	flag.Parse()
	if *consoleLog {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339})
	} else {
		log.Logger = zerolog.Nop()
	}
	lvl, err := zerolog.ParseLevel(*level)
	check(err, "Invalid log level")
	zerolog.SetGlobalLevel(lvl)
	log.Debug().Str("level", *level).Msg("Log level set")

	conn := MakeConnection()

	u, err = url.Parse(*uri)
	check(err, fmt.Sprintf("Error parsing URL '%s'", *uri))
	conn.Request.Url = *u
	conn.Request.Body = *body
	conn.Request.Headers = headers
	conn.Request.Method = *method
	conn.Timeout = *responseTimeout

	log.Info().Str("device_name", *deviceName).Msg("starting up")

	d, err := gatt.NewDevice(option.DefaultClientOptions...)
	check(err, "NewDevice failed")

	// Register handlers.
	d.Handle(
		gatt.PeripheralDiscovered(onPeriphDiscovered),
		gatt.PeripheralConnected(conn.onPeriphConnected),
		gatt.PeripheralDisconnected(onPeriphDisconnected),
	)

	d.Init(onStateChanged)
	<-done
	log.Info().Msg("Done")
}
