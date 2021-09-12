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
			log.Printf("DEBUG : %s : write URI: %s", gatt.UUID16(hps.HTTPURIID), request.URI)
			return gatt.StatusSuccess
		})

	// Headers
	hc := s.AddCharacteristic(gatt.UUID16(hps.HTTPHeadersID))
	hc.HandleWriteFunc(
		func(r gatt.Request, data []byte) (status byte) {
			request.Headers = string(data)
			log.Printf("DEBUG : %s : Write headers : %v", gatt.UUID16(hps.HTTPHeadersID), request.Headers)
			return gatt.StatusSuccess
		})
	hc.HandleReadFunc(
		func(rsp gatt.ResponseWriter, req *gatt.ReadRequest) {
			if response != nil {
				log.Printf("DEBUG : %s : Read headers: %v", gatt.UUID16(hps.HTTPHeadersID), response.DecodedHeaders())
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
			request.Body = data
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
			request.Method, err = hps.DecodeHttpMethod(data[0])
			if err != nil {
				log.Printf("ERROR : %s : HTTP method : %v", gatt.UUID16(hps.HTTPControlPointID), err)
				return gatt.StatusUnexpectedError // TODO is this correct?
			}
			log.Printf("DEBUG : %s : HTTP method : %s", gatt.UUID16(hps.HTTPControlPointID), request.Method)

			request.Scheme, err = hps.DecodeURLScheme(data[0])
			if err != nil {
				log.Printf("ERROR : %s : URL scheme : %v", gatt.UUID16(hps.HTTPControlPointID), err)
				return gatt.StatusUnexpectedError // TODO is this correct?
			}
			log.Printf("DEBUG : %s : URL scheme : %s", gatt.UUID16(hps.HTTPControlPointID), request.Scheme)

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

var (
	poweredOn bool
)

func main() {

	flag.Parse()
	log.Printf("INFO : Device name: %s", *deviceName)
	d, err := gatt.NewDevice(option.DefaultServerOptions...)
	if err != nil {
		log.Fatalf("FATAL : Failed to open device, err: %s", err)
	}

	// Init space for the next request
	request = &savedRequest{}

	// Register optional handlers.
	d.Handle(
		gatt.CentralConnected(func(c gatt.Central) { log.Println("INFO : Connect: ", c.ID()) }),
		gatt.CentralDisconnected(func(c gatt.Central) { log.Println("INFO : Disconnect: ", c.ID()) }),
	)

	// A mandatory handler for monitoring device state.
	onStateChanged := func(d gatt.Device, s gatt.State) {
		log.Printf("INFO : State: %s\n", s)
		switch s {
		case gatt.StatePoweredOn:
			poweredOn = true
			s1 := NewHPSService()
			d.AddService(s1)
			log.Printf("INFO : Server UUID: %s", s1.UUID())
			go advertisePeriodically(d, hps.DeviceName, []gatt.UUID{s1.UUID()})

		default:
			poweredOn = false
		}
	}

	d.Init(onStateChanged)
	select {}
}

func advertisePeriodically(d gatt.Device, deviceName string, services []gatt.UUID) {
	log.Println("INFO : Start advertising")
	for poweredOn {
		// Advertise device name and service's UUIDs.
		d.AdvertiseNameAndServices(hps.DeviceName, services)
		time.Sleep(time.Millisecond * 100)
	}
	log.Println("INFO : Stop advertising")
}
