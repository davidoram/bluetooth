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
	responseTimeout = flag.Duration("timeout", time.Second*10, "Time to wait for server to return response")
	level = flag.String("log-level", "info", "Logging level, eg: panic, fatal, error, warn, info, debug, trace")
	consoleLog = flag.Bool("log-console", false, "Pass true to enable colorized console logging, false for JSON style logging")
}

type ScanStatus struct {
	IsScanning  bool
	FoundServer bool
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

	conn := hps.MakeConnection(*responseTimeout)

	u, err = url.Parse(*uri)
	check(err, fmt.Sprintf("Error parsing URL '%s'", *uri))
	conn.Request.Url = *u
	conn.Request.Body = *body
	conn.Request.Headers = headers
	conn.Request.Method = *method
	resp, err := conn.Connect()
	if err != nil {
		log.Err(err).Msg("Call failed")
		os.Exit(1)
	}
	log.Info().Int("status_code", resp.NotifyStatus.StatusCode).Msg("Done")

}
