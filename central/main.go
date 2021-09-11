package main

/*
 * Central is the client component
 * Takes command line options and translates then to bluetooth calls to the server
 *
 */

import (
	"flag"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"github.com/davidoram/bluetooth/hps"
	"github.com/paypal/gatt"
	"github.com/paypal/gatt/examples/option"
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
	verb    *string

	responseTimeout *time.Duration

	responseChannel = make(chan bool, 1)

	done = make(chan struct{})
)

func init() {
	// id = flag.String("id", hps.PeripheralID, "Peripheral ID to scan for")
	deviceName = flag.String("name", hps.DeviceName, "Device name to scan for")
	uri = flag.String("uri", "http://localhost:8100/hello.txt", "uri")
	flag.Var(&headers, "header", `HTTP headers. eg: -header "Accept=text/plain" -header "X-API-KEY=xyzabc"`)
	body = flag.String("body", "", "HTTP body to POST/PUT")
	verb = flag.String("verb", "GET", "HTTP verb, eg: GET, PUT, POST, PATCH, DELETE")
	responseTimeout = flag.Duration("timeout", time.Second*30, "Time to wait for server to return response")
}

func onStateChanged(d gatt.Device, s gatt.State) {
	log.Printf("DEBUG : State: %v", s)
	switch s {
	case gatt.StatePoweredOn:
		log.Printf("DEBUG : Start scanning...")
		d.Scan([]gatt.UUID{}, false)
		return
	default:
		log.Printf("DEBUG : Stop scanning")
		d.StopScanning()
	}
}
func onPeriphDiscovered(p gatt.Peripheral, a *gatt.Advertisement, rssi int) {
	if p.Name() != *deviceName {
		log.Printf("DEBUG : Skipping Peripheral ID:%s, NAME:%s", p.ID(), p.Name())
		return
	}

	// Stop scanning once we've got the peripheral we're looking for.
	p.Device().StopScanning()

	log.Printf("INFO : Found server with name %s", *deviceName)
	log.Println("DEBUG : ")
	log.Printf("DEBUG : Peripheral ID:%s, NAME:(%s)\n", p.ID(), p.Name())
	log.Println("DEBUG :   Local Name        =", a.LocalName)
	log.Println("DEBUG :   TX Power Level    =", a.TxPowerLevel)
	log.Println("DEBUG :   Manufacturer Data =", a.ManufacturerData)
	log.Println("DEBUG :   Service Data      =", a.ServiceData)
	log.Println("DEBUG : ")

	p.Device().Connect(p)
}

var (
	hpsService *gatt.Service
)

func onPeriphConnected(p gatt.Peripheral, err error) {
	log.Println("DEBUG : Connected")
	defer p.Device().CancelConnection(p)

	if err := p.SetMTU(500); err != nil {
		log.Printf("WARN : Failed to set MTU, err: %s", err)
	}

	// Discovery services
	ss, err := p.DiscoverServices(nil)
	if err != nil {
		log.Printf("WARN : Failed to discover services, err: %s", err)
		return
	}

	for _, s := range ss {
		if s.UUID().Equal(gatt.MustParseUUID(hps.HpsServiceID)) {
			hpsService = s
			err := parseService(p)
			if err != nil {
				log.Printf("ERROR : parsing service: %s", err)
				continue
			}
			err = callService(p)
			if err != nil {
				log.Printf("ERROR : calling service: %s", err)
			}
			break
		}
	}
}

var (
	uriChr, hdrsChr, bodyChr, controlChr, statusChr *gatt.Characteristic
)

func parseService(p gatt.Peripheral) error {
	log.Println("DEBUG : parse service")

	// Discovery characteristics
	cs, err := p.DiscoverCharacteristics(nil, hpsService)
	if err != nil {
		return err
	}
	for _, c := range cs {
		msg := "Characteristic  " + c.UUID().String()
		name := c.Name()
		log.Println("DEBUG : %s %s", msg, name)
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

		// Read the characteristic, if possible.
		if (c.Properties() & gatt.CharRead) != 0 {
			b, err := p.ReadCharacteristic(c)
			if err != nil {
				fmt.Println("WARN : Failed to read characteristic %s, name: %s, err: %s", c.UUID().String(), c.Name(), err)
				continue
			}
			fmt.Printf("    value         %x | %q\n", b, b)
		}

		// Discovery descriptors
		ds, err := p.DiscoverDescriptors(nil, c)
		if err != nil {
			fmt.Printf("WARN : Failed to discover descriptors, err: %s\n", err)
			continue
		}

		for _, d := range ds {
			msg := "DEBUG :   Descriptor      " + d.UUID().String()
			if len(d.Name()) > 0 {
				msg += " (" + d.Name() + ")"
			}
			fmt.Println(msg)

			// Read descriptor (could fail, if it's not readable)
			b, err := p.ReadDescriptor(d)
			if err != nil {
				fmt.Printf("DEBUG : Failed to read descriptor, err: %s\n", err)
				continue
			}
			fmt.Printf("DEBUG :     value         %x | %q\n", b, b)
		}

		// Subscribe the characteristic, if possible.
		if (c.Properties() & (gatt.CharNotify | gatt.CharIndicate)) != 0 {
			f := func(c *gatt.Characteristic, b []byte, err error) {
				fmt.Printf("DEBUG : notified: % X | %q\n", b, b)
				if c.UUID().Equal(gatt.UUID16(hps.HTTPStatusCodeID)) {
					ns, err := hps.DecodeNotifyStatus(b)
					if err != nil {
						fmt.Printf("ERROR : decoding NotifyStatus %v\n", err)
						return
					}
					fmt.Printf("INFO : Status code : %d\n", ns.StatusCode)
					fmt.Printf("INFO : Headers received : %t\n", ns.HeadersReceived)
					fmt.Printf("INFO : Headers truncated: %t\n", ns.HeadersTruncated)
					fmt.Printf("INFO : Body received : %t\n", ns.BodyReceived)
					fmt.Printf("INFO : Body truncated: %t\n", ns.BodyTruncated)
					responseChannel <- true
				}
			}
			if err := p.SetNotifyValue(c, f); err != nil {
				fmt.Printf("WARN : Failed to subscribe characteristic, err: %s\n", err)
				continue
			}
		}

	}
	return nil
}

func callService(p gatt.Peripheral) error {
	log.Println("DEBUG : call service")

	log.Println("DEBUG : set URI")
	urlStr := fmt.Sprintf("%s%s", u.Host, u.EscapedPath())
	err := p.WriteCharacteristic(uriChr, []byte(urlStr), true)
	if err != nil {
		return err
	}

	log.Println("DEBUG : set Headers")
	err = p.WriteCharacteristic(hdrsChr, []byte(headers.String()), true)
	if err != nil {
		return err
	}

	log.Println("DEBUG : set Body")
	err = p.WriteCharacteristic(bodyChr, []byte(*body), true)
	if err != nil {
		return err
	}

	log.Println("DEBUG : set Control")
	code, err := hps.EncodeMethodScheme(*verb, u.Scheme)
	if err != nil {
		return err
	}
	err = p.WriteCharacteristic(controlChr, []byte{code}, false)
	if err != nil {
		return err
	}

	log.Printf("DEBUG : Waiting for %s, for notifiations", responseTimeout)
	time.AfterFunc(*responseTimeout, func() {
		log.Printf("INFO : Timeout")
		responseChannel <- false
	})
	gotResponse := <-responseChannel
	if gotResponse {
		body, err := p.ReadCharacteristic(bodyChr)
		if err != nil {
			return err
		}
		log.Printf("INFO : Body : %v", body)

		hdrBytes, err := p.ReadCharacteristic(hdrsChr)
		if err != nil {
			return err
		}
		headers := hps.DecodeHeaders(hdrBytes)
		log.Printf("INFO : Headers : %v", headers)

	}
	return nil
}

func onPeriphDisconnected(p gatt.Peripheral, err error) {
	log.Println("INFO : Disconnected")
	close(done)
}

func main() {
	flag.Parse()
	var err error
	u, err = url.Parse(*uri)
	if err != nil {
		log.Fatalf("ERROR : Invalid URL : %v", err)
		return
	}
	log.Printf("INFO : Device Name: %s", *deviceName)

	d, err := gatt.NewDevice(option.DefaultClientOptions...)
	if err != nil {
		log.Fatalf("ERROR : Failed to open device, err: %s", err)
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
	log.Println("INFO : Done")
}
