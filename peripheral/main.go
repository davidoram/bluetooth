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
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/davidoram/bluetooth/hps"
	"github.com/paypal/gatt"
	"github.com/paypal/gatt/examples/option"
)

var (
	// id *string
	deviceName *string
)

func init() {
	// id = flag.String("id", hps.PeripheralID, "Peripheral ID")
	deviceName = flag.String("name", hps.DeviceName, "Device name to advertise")

}

func onStateChanged(device gatt.Device, s gatt.State) {
	switch s {
	case gatt.StatePoweredOn:
		log.Println("Scanning for Bluetooth Broadcasts...")
		device.Scan([]gatt.UUID{}, true)
		return
	default:
		device.StopScanning()
	}
}

type savedRequest struct {
	uri      string
	headers  string
	body     []byte
	verb     string
	protocol string
}

type savedResponse struct {
	NotifyStatus hps.NotifyStatus
	Headers      []byte
	Body         []byte
	Notified     bool
}

var (
	request  *savedRequest
	response *savedResponse
)

func sendRequest(r savedRequest) error {

	response = nil

	// Create client
	client := &http.Client{}

	// Create request
	req, err := http.NewRequest(r.verb, fmt.Sprintf("%s://%s", r.protocol, r.uri), bytes.NewReader(r.body))

	// Headers
	if r.headers != "" {
		for _, h := range strings.Split(r.headers, "\n") {
			values := strings.Split(h, "=")
			if len(values) != 2 {
				log.Printf("Ignoring invalid header %v", h)
				continue
			}
			req.Header.Add(values[0], values[1])
		}
	}

	// Fetch Request
	log.Printf("DBEUG : Proxying request %v ", req)
	resp, err := client.Do(req)

	if err != nil {
		log.Println("ERROR : HTTP call failed: ", err)
		return err
	}

	// Read Response Body
	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Printf("ERROR : Reading response body: %v", err)
		return err
	}

	b, trunc := hps.EncodeHeaders(resp.Header)
	response = &savedResponse{
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
			request.uri = string(data)
			log.Printf("DEBUG : %s :write URI: %s", gatt.UUID16(hps.HTTPURIID), request.uri)
			return gatt.StatusSuccess
		})

	// Headers
	hc := s.AddCharacteristic(gatt.UUID16(hps.HTTPHeadersID))
	hc.HandleWriteFunc(
		func(r gatt.Request, data []byte) (status byte) {
			request.headers = string(data)
			log.Printf("DEBUG : %s : Write headers : %v", gatt.UUID16(hps.HTTPHeadersID), request.headers)
			return gatt.StatusSuccess
		})
	hc.HandleReadFunc(
		func(rsp gatt.ResponseWriter, req *gatt.ReadRequest) {
			if response != nil {
				log.Printf("DEBUG : %s : Read headers: %v", gatt.UUID16(hps.HTTPHeadersID), string(response.Headers))
				_, err := rsp.Write(response.Headers)
				if err != nil {
					log.Printf("ERROR : %s : HTTP Header read err: %v", gatt.UUID16(hps.HTTPHeadersID), err)
				}
			} else {
				log.Printf("WARN : %s : Read HTTP Header before response received", gatt.UUID16(hps.HTTPHeadersID))
			}
		})

	// Body
	hb := s.AddCharacteristic(gatt.UUID16(hps.HTTPEntityBodyID))
	hb.HandleWriteFunc(
		func(r gatt.Request, data []byte) (status byte) {
			request.body = data
			log.Printf("DEBUG : %s : Write body: %v", gatt.UUID16(hps.HTTPEntityBodyID), data)
			return gatt.StatusSuccess
		})
	hb.HandleReadFunc(
		func(rsp gatt.ResponseWriter, req *gatt.ReadRequest) {
			if response != nil {
				log.Printf("DEBUG : %s : Read body: %v", gatt.UUID16(hps.HTTPEntityBodyID), string(response.Body))
				_, err := rsp.Write(response.Body)
				if err != nil {
					log.Printf("ERROR : %s : HTTP Body read err: %v", gatt.UUID16(hps.HTTPEntityBodyID), err)
				}
			} else {
				log.Printf("WARN : %s : Read HTTP Body before response received", gatt.UUID16(hps.HTTPEntityBodyID))
			}
		})

	// Receive control point, this triggers the HTTP request to occur
	s.AddCharacteristic(gatt.UUID16(hps.HTTPControlPointID)).HandleWriteFunc(
		func(r gatt.Request, data []byte) (status byte) {
			var err error
			request.verb, err = hps.DecodeHttpMethod(data[0])
			if err != nil {
				log.Printf("ERROR : %s : HTTP method : %v", gatt.UUID16(hps.HTTPControlPointID), err)
				return gatt.StatusUnexpectedError // TODO is this correct?
			}
			log.Printf("DEBUG : %s : HTTP method : %s", gatt.UUID16(hps.HTTPControlPointID), request.verb)

			request.protocol, err = hps.DecodeURLScheme(data[0])
			if err != nil {
				log.Printf("ERROR : %s : URL scheme : %v", gatt.UUID16(hps.HTTPControlPointID), err)
				return gatt.StatusUnexpectedError // TODO is this correct?
			}
			log.Printf("DEBUG : %s : URL scheme : %s", gatt.UUID16(hps.HTTPControlPointID), request.verb)

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
					log.Printf("DEBUG : %s : Sending response %d", gatt.UUID16(hps.HTTPStatusCodeID), response.NotifyStatus.StatusCode)
					_, err := n.Write(response.NotifyStatus.Encode())
					if err != nil {
						log.Printf("ERROR : %s : sending response %v", gatt.UUID16(hps.HTTPStatusCodeID), err)
					}
					response.Notified = true
				} else {
					time.Sleep(time.Millisecond * 100)
				}
			}
		})

	return s
}

func main() {
	flag.Parse()
	log.Printf("Device name: %s", *deviceName)
	d, err := gatt.NewDevice(option.DefaultServerOptions...)
	if err != nil {
		log.Fatalf("Failed to open device, err: %s", err)
	}

	// Init space for the next request
	request = &savedRequest{}

	// Register optional handlers.
	d.Handle(
		gatt.CentralConnected(func(c gatt.Central) { log.Println("Connect: ", c.ID()) }),
		gatt.CentralDisconnected(func(c gatt.Central) { log.Println("Disconnect: ", c.ID()) }),
	)

	// A mandatory handler for monitoring device state.
	onStateChanged := func(d gatt.Device, s gatt.State) {
		fmt.Printf("State: %s\n", s)
		switch s {
		case gatt.StatePoweredOn:
			s1 := NewHPSService()
			d.AddService(s1)
			log.Printf("Server UUID: %s", s1.UUID())

			// Advertise device name and service's UUIDs.
			d.AdvertiseNameAndServices(hps.DeviceName, []gatt.UUID{s1.UUID()})

		default:
		}
	}

	d.Init(onStateChanged)
	select {}
}
