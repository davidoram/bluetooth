package main

/*
 * Central is the client component
 * Takes command line options and translates then to bluetooth calls to the server
 *
 */

import (
	"errors"
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

type arrayStr []string

var (
	// id          *string
	deviceName *string

	uri     *string
	u       *url.URL
	headers arrayStr
	body    *string
	verb    *string

	responseChannel = make(chan bool, 1)

	done = make(chan struct{})
)

func (i *arrayStr) String() string {
	return strings.Join([]string(*i), "\n")
}

func (i *arrayStr) Set(value string) error {
	if len(strings.Split(value, "=")) != 2 {
		return fmt.Errorf("Invalid header format, expect 'key=value'")
	}
	*i = append(*i, value)
	return nil
}

func init() {
	// id = flag.String("id", hps.PeripheralID, "Peripheral ID to scan for")
	deviceName = flag.String("name", hps.DeviceName, "Device name to scan for")
	uri = flag.String("uri", "http://localhost:8100/hello.txt", "uri")
	flag.Var(&headers, "header", `HTTP headers. eg: -header "Accept=text/plain" -header "X-API-KEY=xyzabc"`)
	body = flag.String("body", "", "HTTP body to POST/PUT")
	verb = flag.String("verb", "GET", "HTTP verb, eg: GET, PUT, POST, PATCH, DELETE")
}

func onStateChanged(d gatt.Device, s gatt.State) {
	fmt.Println("State:", s)
	switch s {
	case gatt.StatePoweredOn:
		fmt.Println("Scanning...")
		d.Scan([]gatt.UUID{}, false)
		return
	default:
		d.StopScanning()
	}
}
func onPeriphDiscovered(p gatt.Peripheral, a *gatt.Advertisement, rssi int) {
	if p.Name() != *deviceName {
		log.Printf("Skipping Peripheral ID:%s, NAME:%s", p.ID(), p.Name())
		return
	}

	// Stop scanning once we've got the peripheral we're looking for.
	p.Device().StopScanning()

	log.Printf("Found server")
	log.Printf("\nPeripheral ID:%s, NAME:(%s)\n", p.ID(), p.Name())
	log.Println("  Local Name        =", a.LocalName)
	log.Println("  TX Power Level    =", a.TxPowerLevel)
	log.Println("  Manufacturer Data =", a.ManufacturerData)
	log.Println("  Service Data      =", a.ServiceData)
	log.Println("")

	p.Device().Connect(p)
}

var (
	hpsService *gatt.Service
)

func onPeriphConnected(p gatt.Peripheral, err error) {
	log.Println("Connected")
	defer p.Device().CancelConnection(p)

	if err := p.SetMTU(500); err != nil {
		log.Printf("Failed to set MTU, err: %s\n", err)
	}

	// Discovery services
	ss, err := p.DiscoverServices(nil)
	if err != nil {
		log.Printf("Failed to discover services, err: %s\n", err)
		return
	}

	for _, s := range ss {
		if s.UUID().Equal(gatt.MustParseUUID(hps.HpsServiceID)) {
			hpsService = s
			parseService(p)
			err := callService(p)
			if err != nil {
				log.Printf("Error calling service: %s", err)
			}
			break
		}
	}
}

var (
	uriChr, hdrsChr, bodyChr, controlChr, statusChr *gatt.Characteristic
)

func parseService(p gatt.Peripheral) {
	log.Println("parse service")

	// Discovery characteristics
	cs, err := p.DiscoverCharacteristics(nil, hpsService)
	if err != nil {
		log.Printf("Error: Failed to discover characteristics, err: %s\n", err)
	}
	log.Printf("Service has %d characteristics", len(cs))
	for _, c := range cs {
		msg := "Characteristic  " + c.UUID().String()
		name := c.Name()
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
				fmt.Printf("Failed to read characteristic, err: %s\n", err)
				continue
			}
			fmt.Printf("    value         %x | %q\n", b, b)
		}

		// Discovery descriptors
		ds, err := p.DiscoverDescriptors(nil, c)
		if err != nil {
			fmt.Printf("Failed to discover descriptors, err: %s\n", err)
			continue
		}

		for _, d := range ds {
			msg := "  Descriptor      " + d.UUID().String()
			if len(d.Name()) > 0 {
				msg += " (" + d.Name() + ")"
			}
			fmt.Println(msg)

			// Read descriptor (could fail, if it's not readable)
			b, err := p.ReadDescriptor(d)
			if err != nil {
				fmt.Printf("Failed to read descriptor, err: %s\n", err)
				continue
			}
			fmt.Printf("    value         %x | %q\n", b, b)
		}

		// Subscribe the characteristic, if possible.
		if (c.Properties() & (gatt.CharNotify | gatt.CharIndicate)) != 0 {
			f := func(c *gatt.Characteristic, b []byte, err error) {
				fmt.Printf("notified: % X | %q\n", b, b)
				if c.UUID().Equal(gatt.UUID16(hps.HTTPStatusCodeID)) {
					ns, err := hps.DecodeNotifyStatus(b)
					if err != nil {
						fmt.Printf("Error decoding NotifyStatus %v\n", err)
						return
					}
					fmt.Printf("Status code : %d\n", ns.StatusCode)
					fmt.Printf("Headers received : %t\n", ns.HeadersReceived)
					fmt.Printf("Headers truncated: %t\n", ns.HeadersTruncated)
					fmt.Printf("Body received : %t\n", ns.BodyReceived)
					fmt.Printf("Body truncated: %t\n", ns.BodyTruncated)
					responseChannel <- true
				}
			}
			if err := p.SetNotifyValue(c, f); err != nil {
				fmt.Printf("Failed to subscribe characteristic, err: %s\n", err)
				continue
			}
		}

		log.Printf("%s %s", msg, name)
	}

}

func callService(p gatt.Peripheral) error {
	log.Println("call service")

	log.Println("set URI")
	urlStr := fmt.Sprintf("%s%s", u.Host, u.EscapedPath())
	err := p.WriteCharacteristic(uriChr, []byte(urlStr), true)
	if err != nil {
		log.Printf("Error: Setting URI, err: %s", err)
		return err
	}
	// _, err = p.ReadCharacteristic(uriChr)
	// if err != nil {
	// 	log.Printf("Error: Reading URI response, err: %s", err)
	// 	return err
	// }

	log.Println("set Headers")
	err = p.WriteCharacteristic(hdrsChr, []byte(headers.String()), true)
	if err != nil {
		log.Printf("Error: Setting Headers, err: %s", err)
		return err
	}

	log.Println("set Body")
	err = p.WriteCharacteristic(bodyChr, []byte(*body), true)
	if err != nil {
		log.Printf("Error: Setting Body, err: %s", err)
		return err
	}

	log.Println("set control")
	code, err := verbPayload(*verb, u.Scheme)
	if err != nil {
		log.Printf("Error: Parsing verb: %s, scheme: %s, err: %s", *verb, u.Scheme, err)
		return err
	}
	err = p.WriteCharacteristic(controlChr, []byte{code}, false)
	if err != nil {
		log.Printf("Error: Setting URI, err: %s", err)
		return err
	}

	// buf, err := p.ReadCharacteristic(controlChr)
	// if err != nil {
	// 	log.Printf("Error: Reading Control response, err: %s", err)
	// 	return err
	// }
	// log.Printf("Response: %v", buf)
	log.Printf("Waiting for 5 seconds to get some notifiations, if any.\n")
	time.AfterFunc(5*time.Second, func() {
		log.Printf("Timeout\n")
		responseChannel <- false
	})
	gotResponse := <-responseChannel
	if gotResponse {
		log.Println("Read body")
		body, err := p.ReadCharacteristic(bodyChr)
		if err != nil {
			log.Printf("Error: Reading Body response, err: %s", err)
			return err
		}
		log.Printf("Got Body %s", string(body))

		hdrBytes, err := p.ReadCharacteristic(hdrsChr)
		if err != nil {
			log.Printf("Error: Reading Headers response, err: %s", err)
			return err
		}
		headers := hps.DecodeHeaders(hdrBytes)
		log.Printf("Got Headers %v", headers)

	}
	return nil
}

var (
	UnsupportedSchemeError   = errors.New("Unsupported scheme, valid values are http and https")
	UnsupportedHttpVerbError = errors.New("Unsupported verb, valid values are get, head, post, put, delete")
)

func verbPayload(verb, scheme string) (uint8, error) {
	switch strings.ToLower(strings.Trim(verb, " ")) {
	case "get":
		switch scheme {
		case "http":
			return hps.HTTPGet, nil
		case "https":
			return hps.HTTPSGet, nil
		default:
			return 0, UnsupportedSchemeError
		}
	case "head":
		switch scheme {
		case "http":
			return hps.HTTPHead, nil
		case "https":
			return hps.HTTPSHead, nil
		default:
			return 0, UnsupportedSchemeError
		}
	case "post":
		switch scheme {
		case "http":
			return hps.HTTPPost, nil
		case "https":
			return hps.HTTPSPost, nil
		default:
			return 0, UnsupportedSchemeError
		}
	case "put":
		switch scheme {
		case "http":
			return hps.HTTPPut, nil
		case "https":
			return hps.HTTPSPut, nil
		default:
			return 0, UnsupportedSchemeError
		}
	case "delete":
		switch scheme {
		case "http":
			return hps.HTTPDelete, nil
		case "https":
			return hps.HTTPSDelete, nil
		default:
			return 0, UnsupportedSchemeError
		}
	default:
		return 0, UnsupportedHttpVerbError
	}
}

func onPeriphDisconnected(p gatt.Peripheral, err error) {
	log.Println("Disconnected")
	close(done)
}

func main() {
	flag.Parse()
	var err error
	u, err = url.Parse(*uri)
	if err != nil {
		log.Fatalf("URL parse error: %v", err)
		return
	}
	log.Printf("Device Name: %s", *deviceName)

	d, err := gatt.NewDevice(option.DefaultClientOptions...)
	if err != nil {
		log.Fatalf("Failed to open device, err: %s\n", err)
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
	log.Println("Done")
}
