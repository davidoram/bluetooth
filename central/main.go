package main

/*
 * Central is the client component
 * Takes command line options and translates then to bluetooth calls to the server
 *
 */

import (
	"flag"
	"log"
	"net/url"
	"os"
	"time"

	"github.com/davidoram/bluetooth/hps"
)

var (
	deviceName *string

	uri     *string
	u       *url.URL
	headers hps.ArrayStr
	body    *string
	method  *string

	responseTimeout *time.Duration
)

func init() {
	log.SetOutput(os.Stdout)

	// id = flag.String("id", hps.PeripheralID, "Peripheral ID to scan for")
	deviceName = flag.String("name", hps.DeviceName, "Device name to scan for")
	uri = flag.String("uri", "http://localhost:8100/hello.txt", "uri")
	flag.Var(&headers, "header", `HTTP headers. eg: -header "Accept=text/plain" -header "X-API-KEY=xyzabc"`)
	body = flag.String("body", "", "HTTP body to POST/PUT")
	method = flag.String("verb", "GET", "HTTP verb, eg: GET, PUT, POST, PATCH, DELETE")
	responseTimeout = flag.Duration("timeout", time.Second*5, "Time to wait for server to return response")

}

func main() {
	flag.Parse()

	u, err := url.Parse(*uri)
	if err != nil {
		log.Printf("URI Parse error: %s", err)
		return
	}
	c := hps.MakeClient()
	headers := hps.ArrayStr{}
	_, err = c.Do(u.String(), *body, *method, headers)
	if err != nil {
		log.Printf("Error: %s", err)
		return
	}
	log.Printf("Ok")
}
