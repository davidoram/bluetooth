package hps

// Package hps provides HPS/HTTP client  implementations
import (
	"errors"
	"fmt"
	"log"
	"net/url"
	"time"

	"github.com/paypal/gatt"
	"github.com/paypal/gatt/examples/option"
)

var (
	UnknownError = errors.New("Unknown error")
)

type Client struct {
	DebugLog        bool
	DeviceName      string
	ResponseTimeout time.Duration

	uri     string
	u       *url.URL
	headers ArrayStr
	body    string
	method  string

	responseChannel chan bool
	response        *Response
	lastError       error

	foundServer bool
	hpsService  *gatt.Service

	uriChr, hdrsChr, bodyChr, controlChr, statusChr *gatt.Characteristic

	done chan bool
}

func MakeClient() *Client {

	c := Client{
		DeviceName:      DeviceName,
		lastError:       UnknownError,
		responseChannel: make(chan bool, 1),
		done:            make(chan bool, 1),
		response:        &Response{},
	}
	c.ResponseTimeout, _ = time.ParseDuration("5s")
	return &c
}

func (client *Client) Do(uri, body, method string, headers ArrayStr) (Response, error) {
	client.uri = uri
	client.u, client.lastError = url.Parse(client.uri)
	if client.lastError != nil {
		log.Printf("Error Parsing URI, err: %v", client.lastError)
		return *client.response, client.lastError
	}
	client.method = method
	client.body = body
	client.headers = headers

	var d gatt.Device
	d, client.lastError = gatt.NewDevice(option.DefaultClientOptions...)
	if client.lastError != nil {
		return *client.response, client.lastError
	}

	// Register handlers.
	d.Handle(
		gatt.PeripheralDiscovered(client.onPeriphDiscovered),
		gatt.PeripheralConnected(client.onPeriphConnected),
		gatt.PeripheralDisconnected(client.onPeriphDisconnected),
	)

	d.Init(client.onStateChanged)
	<-client.done
	return *client.response, client.lastError
}

func (client *Client) onStateChanged(d gatt.Device, s gatt.State) {
	log.Printf("state changed to %s", s.String())
	switch s {
	case gatt.StatePoweredOn:
		go client.scanPeriodically(d)
	default:
		d.StopScanning()
	}
}

func (client *Client) scanPeriodically(d gatt.Device) {
	log.Printf("start periodic scan")
	for !client.foundServer {
		d.Scan([]gatt.UUID{}, false)
		time.Sleep(time.Millisecond * 100)
	}
	log.Printf("stop periodic scan")
}

func (client *Client) onPeriphDiscovered(p gatt.Peripheral, a *gatt.Advertisement, rssi int) {
	if p.Name() != client.DeviceName {
		log.Printf("Skip peripheral_id: %s, name: %s", p.ID(), p.Name())
		return
	}
	client.foundServer = true

	// Stop scanning once we've got the peripheral we're looking for.
	log.Printf("Found HPS server")
	p.Device().StopScanning()
	p.Device().Connect(p)
}

func (client *Client) onPeriphConnected(p gatt.Peripheral, err error) {
	log.Printf("connected")

	if client.lastError = p.SetMTU(500); client.lastError != nil {
		log.Printf("Error setting MTU, err: %v", client.lastError)
		return
	}

	// Discovery services
	var ss []*gatt.Service
	ss, client.lastError = p.DiscoverServices(nil)
	if client.lastError != nil {
		log.Printf("Error Discover services, err: %v", client.lastError)
		return
	}

	for _, s := range ss {
		if s.UUID().Equal(gatt.MustParseUUID(HpsServiceID)) {
			client.hpsService = s
			err := client.parseService(p)
			if err != nil {
				log.Printf("Warning Parsing service, err: %v", err)
				continue
			}
			client.lastError = client.callService(p)
			if client.lastError != nil {
				log.Printf("Error Calling service, err: %v", client.lastError)
			}
			break
		}
	}
}

func (client *Client) onPeriphDisconnected(p gatt.Peripheral, err error) {
	log.Printf("disconnected")
	close(client.done)
}

func (client *Client) parseService(p gatt.Peripheral) error {
	log.Printf("parse service")

	// Discovery characteristics
	var cs []*gatt.Characteristic
	cs, client.lastError = p.DiscoverCharacteristics(nil, client.hpsService)
	if client.lastError != nil {
		return client.lastError
	}
	for _, c := range cs {
		log.Printf("discovered characteristic name: %s", c.Name())
		switch c.UUID().String() {
		case gatt.UUID16(HTTPURIID).String():
			client.uriChr = c
		case gatt.UUID16(HTTPHeadersID).String():
			client.hdrsChr = c
		case gatt.UUID16(HTTPEntityBodyID).String():
			client.bodyChr = c
		case gatt.UUID16(HTTPControlPointID).String():
			client.controlChr = c
		case gatt.UUID16(HTTPStatusCodeID).String():
			client.statusChr = c
		}

		// Discovery descriptors
		ds, err := p.DiscoverDescriptors(nil, c)
		if err != nil {
			log.Printf("Warn discover descriptors, err: %v", err)
			continue
		}

		for _, d := range ds {
			// Read descriptor (could fail, if it's not readable)
			_, err := p.ReadDescriptor(d)
			if err != nil {
				log.Printf("Warn reading descriptor: %s, err: %v", d.Name(), err)
				continue
			}
		}

		// Subscribe the characteristic, if possible.
		if (c.Properties() & (gatt.CharNotify | gatt.CharIndicate)) != 0 {
			f := func(c *gatt.Characteristic, b []byte, err error) {
				if c.UUID().Equal(gatt.UUID16(HTTPStatusCodeID)) {
					var ns NotifyStatus
					ns, client.lastError = DecodeNotifyStatus(b)
					if client.lastError != nil {
						log.Printf("Error decoding notify status err: %v", client.lastError)
						return
					}
					log.Printf("http_status: %d", ns.StatusCode)
					log.Printf("headers: %v", ns.HeadersReceived)
					log.Printf("headers_truncated: %t", ns.HeadersTruncated)
					log.Printf("body_received: %t", ns.BodyReceived)
					log.Printf("body_truncated: %t", ns.BodyTruncated)
					client.response = &Response{NotifyStatus: ns}
					client.responseChannel <- true
				}
			}
			if client.lastError = p.SetNotifyValue(c, f); client.lastError != nil {
				log.Printf("Error subscribing to notifications, err: %v", client.lastError)
				continue
			}
		}

	}
	return nil
}

func (client *Client) callService(p gatt.Peripheral) error {
	defer p.Device().CancelConnection(p)

	log.Printf("call service")
	log.Printf("%s %s", client.method, client.u.String())
	log.Printf("headers: %v", client.headers)
	log.Printf("method: %v", client.headers)

	urlStr := fmt.Sprintf("%s%s", client.u.Host, client.u.EscapedPath())
	client.lastError = p.WriteCharacteristic(client.uriChr, []byte(urlStr), true)
	if client.lastError != nil {
		return client.lastError
	}

	client.lastError = p.WriteCharacteristic(client.hdrsChr, []byte(client.headers.String()), true)
	if client.lastError != nil {
		return client.lastError
	}

	client.lastError = p.WriteCharacteristic(client.bodyChr, []byte(client.body), true)
	if client.lastError != nil {
		return client.lastError
	}

	var code uint8
	code, client.lastError = EncodeMethodScheme(client.method, client.u.Scheme)
	if client.lastError != nil {
		return client.lastError
	}
	client.lastError = p.WriteCharacteristic(client.controlChr, []byte{code}, false)
	if client.lastError != nil {
		return client.lastError
	}

	log.Printf("waiting for notification, timeout after %d", client.ResponseTimeout)
	time.AfterFunc(client.ResponseTimeout, func() {
		log.Printf("timeout expired, no notification received")
		client.responseChannel <- false
	})
	gotResponse := <-client.responseChannel
	if gotResponse {
		client.response.Body, client.lastError = p.ReadCharacteristic(client.bodyChr)
		if client.lastError != nil {
			return client.lastError
		}
		log.Printf("received body: %s", string(client.response.Body))

		client.response.Headers, client.lastError = p.ReadCharacteristic(client.hdrsChr)
		if client.lastError != nil {
			return client.lastError
		}
		log.Printf("received headers: %s", string(client.response.Headers))

		// all done no errors!
		client.lastError = nil
		client.done <- true
	}
	return nil
}
