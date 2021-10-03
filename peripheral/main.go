package main

/*
 * Peripheral is the server component
 * Accpets HPS requests & calls out to local service
 */

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/davidoram/bluetooth/hps"
	"github.com/paypal/gatt"
	"github.com/paypal/gatt/examples/option"
)

var (
	deviceName *string
)

func init() {
	log.SetOutput(os.Stdout)

	// id = flag.String("id", hps.PeripheralID, "Peripheral ID")
	deviceName = flag.String("name", hps.DeviceName, "Device name to advertise")
}

func onStateChanged(device gatt.Device, s gatt.State) {
	log.Printf("State changed to %s", s.String())
	switch s {
	case gatt.StatePoweredOn:
		log.Printf("start scanning")
		device.Scan([]gatt.UUID{}, true)
		return
	default:
		log.Printf("stop scanning")
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
				log.Printf("Warn: ignoring invalid header %s", h)
				continue
			}
			req.Header.Add(values[0], values[1])
		}
	}

	// Fetch Request
	log.Printf("proxying request")
	resp, err := client.Do(req)

	if err != nil {
		log.Printf("Error: HTTP call failed")
		response = &hps.Response{
			NotifyStatus: hps.NotifyStatus{
				StatusCode:       http.StatusBadGateway,
				HeadersReceived:  false,
				HeadersTruncated: false,
				BodyReceived:     false,
				BodyTruncated:    false,
			},
			Headers: make([]byte, 0),
			Body:    make([]byte, 0),
		}
		return err
	}

	// Read Response Body
	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Error: Read response body failed, err %v", err)
		response = &hps.Response{
			NotifyStatus: hps.NotifyStatus{
				StatusCode:       http.StatusInternalServerError,
				HeadersReceived:  false,
				HeadersTruncated: false,
				BodyReceived:     false,
				BodyTruncated:    false,
			},
			Headers: make([]byte, 0),
			Body:    make([]byte, 0),
		}
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
			log.Printf("url: %s", request.URI)
			return gatt.StatusSuccess
		})

	// Headers
	hc := s.AddCharacteristic(gatt.UUID16(hps.HTTPHeadersID))
	hc.HandleWriteFunc(
		func(r gatt.Request, data []byte) (status byte) {
			request.Headers = string(data)
			log.Printf("write headers: %s", request.Headers)
			return gatt.StatusSuccess
		})
	hc.HandleReadFunc(
		func(rsp gatt.ResponseWriter, req *gatt.ReadRequest) {
			if response != nil {
				_, err := rsp.Write(response.Headers)
				if err != nil {
					log.Printf("Error: Read headers %v", err)
				}
			} else {
				log.Printf("Warn: Read headers received before response has arrived")
			}
		})

	// Body
	hb := s.AddCharacteristic(gatt.UUID16(hps.HTTPEntityBodyID))
	hb.HandleWriteFunc(
		func(r gatt.Request, data []byte) (status byte) {
			request.Body = data
			log.Printf("write body: %s", string(request.Body))
			return gatt.StatusSuccess
		})
	hb.HandleReadFunc(
		func(rsp gatt.ResponseWriter, req *gatt.ReadRequest) {
			if response != nil {
				_, err := rsp.Write(response.Body)
				if err != nil {
					log.Printf("Error: Read body %v", err)
				}
			} else {
				log.Printf("Warn: Read body received before response has arrived")
			}
		})

	// Receive control point, this triggers the HTTP request to occur
	scc := s.AddCharacteristic(gatt.UUID16(hps.HTTPStatusCodeID))
	s.AddCharacteristic(gatt.UUID16(hps.HTTPControlPointID)).HandleWriteFunc(
		func(r gatt.Request, data []byte) (status byte) {
			var err error
			log.Printf("Decoding control %d", uint(data[0]))
			request.Method, err = hps.DecodeHttpMethod(data[0])
			if err != nil {
				log.Printf("Error: Write control %v", err)
				return gatt.StatusUnexpectedError // TODO is this correct?
			}

			request.Scheme, err = hps.DecodeURLScheme(data[0])
			if err != nil {
				log.Printf("Error: Decode scheme %v", err)
				return gatt.StatusUnexpectedError // TODO is this correct?
			}

			// Make the API call in the background
			go sendRequest(*request)

			// Reset inputs, ready for the next call
			request = &savedRequest{}

			return gatt.StatusSuccess
		})

	scc.HandleWriteFunc(func(r gatt.Request, data []byte) (status byte) {
		return gatt.StatusSuccess
	})
	scc.HandleNotifyFunc(
		func(r gatt.Request, n gatt.Notifier) {
			for !n.Done() {
				if response != nil && !response.Notified {
					log.Printf("notify status code: %d", response.NotifyStatus.StatusCode)
					_, err := n.Write(response.NotifyStatus.Encode())
					if err != nil {
						log.Printf("Error: notify status code %v", err)
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

	log.Printf("Make device name: %s", *deviceName)

	d, err := gatt.NewDevice(option.DefaultServerOptions...)
	if err != nil {
		log.Fatalf("Error: new device %v", err)
	}

	// Init space for the next request
	request = &savedRequest{}

	// Register optional handlers.
	d.Handle(
		gatt.CentralConnected(func(c gatt.Central) {
			log.Printf("connected central_id: %s", c.ID())
		}),
		gatt.CentralDisconnected(func(c gatt.Central) {
			log.Printf("disconnected central_id: %s", c.ID())
		}),
	)

	// A mandatory handler for monitoring device state.
	onStateChanged := func(d gatt.Device, s gatt.State) {
		log.Printf("state changed %s", s.String())
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
	log.Printf("start advertising")
	for poweredOn {
		// Advertise device name and service's UUIDs.
		d.AdvertiseNameAndServices(hps.DeviceName, services)
		time.Sleep(time.Millisecond * 100)
	}
	log.Printf("stop advertising")
}
