package main

/*
 * Peripheral is the server component
 * Accpets HPS requests & calls out to local service
 *
 * Started with an uuid identifier

 env GOOS=linux GOARCH=arm64 go build peripheral.go && scp ./peripheral ubuntu@rpi-4b-node1:/home/ubuntu/peripheral
*/

import (
	"encoding/binary"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/paypal/gatt"
	"github.com/paypal/gatt/examples/option"
)

var (
	devices   map[string]gatt.Peripheral
	devices_l sync.Mutex

	id *string
)

const (
	PeripheralID = "b4a77f05-2524-4330-bcbb-5aafd2a9329b"
	HpsServiceID = "0136bd82-ba81-48c6-b608-df7aa274338a"

	// From https://btprodspecificationrefs.blob.core.windows.net/assigned-values/16-bit%20UUID%20Numbers%20Document.pdf
	HTTPURIID          = 0x2AB6
	HTTPHeadersID      = 0x2AB7
	HTTPStatusCodeID   = 0x2AB8
	HTTPEntityBodyID   = 0x2AB9
	HTTPControlPointID = 0x2ABA
	HTTPSSecurityID    = 0x2ABB
	TDSControlPointID  = 0x2ABC

	HTTPReserved      uint8 = 0x00
	HTTPGet           uint8 = 0x01
	HTTPHead          uint8 = 0x02
	HTTPPost          uint8 = 0x03
	HTTPPut           uint8 = 0x04
	HTTPDelete        uint8 = 0x05
	HTTPSGet          uint8 = 0x06
	HTTPSHead         uint8 = 0x07
	HTTPSPost         uint8 = 0x08
	HTTPSPut          uint8 = 0x09
	HTTPSDelete       uint8 = 0x0a
	HTTPRequestCancel uint8 = 0x0b

	DataStatusHeadersReceived  uint8 = 0x01
	DataStatusHeadersTruncated uint8 = 0x02
	DataStatusBodyReceived     uint8 = 0x04
	DataStatusBodyTruncated    uint8 = 0x08
)

func init() {
	id = flag.String("id", PeripheralID, "Peripheral ID")
}

func onStateChanged(device gatt.Device, s gatt.State) {
	switch s {
	case gatt.StatePoweredOn:
		fmt.Println("Scanning for Bluetooth Broadcasts...")
		device.Scan([]gatt.UUID{}, true)
		return
	default:
		device.StopScanning()
	}
}

var (
	uri      string
	headers  string
	body     string
	verb     string
	protocol string

	resp     *http.Response
	respBody []byte
)

func sendRequest() {
	// Create client
	client := &http.Client{}

	// Create request
	req, err := http.NewRequest(verb, fmt.Sprintf("%s://%s", protocol, uri), strings.NewReader(body))

	// Fetch Request
	log.Printf("Fetching request %v ...", req)
	resp, err := client.Do(req)

	if err != nil {
		fmt.Println("Failure : ", err)
	}

	// Read Response Body
	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Error reading response body: %v", err)
	}

	// Display Results
	fmt.Println("response Status : ", resp.Status)
	fmt.Println("response Headers : ", resp.Header)
	fmt.Println("response Body : ", string(respBody))
}

func NewHPSService() *gatt.Service {
	s := gatt.NewService(gatt.MustParseUUID(HpsServiceID))

	// Receive URI
	s.AddCharacteristic(gatt.UUID16(HTTPURIID)).HandleWriteFunc(
		func(r gatt.Request, data []byte) (status byte) {
			uri = string(data)
			log.Println("URI:", uri)
			return gatt.StatusSuccess
		})

	// Receive Headers
	hc := s.AddCharacteristic(gatt.UUID16(HTTPHeadersID))
	hc.HandleWriteFunc(
		func(r gatt.Request, data []byte) (status byte) {
			headers = string(data)
			log.Println("headers:", headers)
			return gatt.StatusSuccess
		})
	hc.HandleReadFunc(
		func(rsp gatt.ResponseWriter, req *gatt.ReadRequest) {
			err := resp.Header.WriteSubset(rsp, nil)
			if err != nil {
				log.Printf("HTTP Header read err: %v", err)
			}
		})

	// Receive Entity Body
	hb := s.AddCharacteristic(gatt.UUID16(HTTPEntityBodyID))
	hb.HandleWriteFunc(
		func(r gatt.Request, data []byte) (status byte) {
			body = string(data)
			log.Println("entityBody:", body)
			return gatt.StatusSuccess
		})
	hb.HandleReadFunc(
		func(rsp gatt.ResponseWriter, req *gatt.ReadRequest) {
			_, err := rsp.Write(respBody)
			if err != nil {
				log.Printf("HTTP body read err: %v", err)
			}
		})

	// Receive control point, this triggers the HTTP request to occur
	s.AddCharacteristic(gatt.UUID16(HTTPControlPointID)).HandleWriteFunc(
		func(r gatt.Request, data []byte) (status byte) {

			switch data[0] {
			case HTTPGet, HTTPSGet:
				verb = "GET"
			case HTTPHead, HTTPSHead:
				verb = "HEAD"
			case HTTPPut, HTTPSPut:
				verb = "PUT"
			case HTTPPost, HTTPSPost:
				verb = "POST"
			case HTTPDelete, HTTPSDelete:
				verb = "DELETE"
			}
			log.Println("verb:", verb)

			switch data[0] {
			case HTTPSGet, HTTPSHead, HTTPSPut, HTTPSPost, HTTPSDelete:
				protocol = "https"
			default:
				protocol = "http"
			}
			log.Println("protocol:", protocol)

			// TODO - perform the HTTP request here
			go sendRequest()

			return gatt.StatusSuccess
		})

	s.AddCharacteristic(gatt.UUID16(HTTPStatusCodeID)).HandleNotifyFunc(
		func(r gatt.Request, n gatt.Notifier) {
			var statusCode uint16 = 200
			var dataStatus uint8 = DataStatusHeadersReceived | DataStatusBodyReceived

			b := make([]byte, 3)
			binary.LittleEndian.PutUint16(b, statusCode)
			b[2] = dataStatus
			_, err := n.Write(b)
			if err != nil {
				log.Printf("HTTP Status notify err: %v", err)
			}
		})

	return s
}

func main() {
	flag.Parse()
	log.Printf("Peripheral ID: %s", *id)
	d, err := gatt.NewDevice(option.DefaultServerOptions...)
	if err != nil {
		log.Fatalf("Failed to open device, err: %s", err)
	}

	// Register optional handlers.
	d.Handle(
		gatt.CentralConnected(func(c gatt.Central) { fmt.Println("Connect: ", c.ID()) }),
		gatt.CentralDisconnected(func(c gatt.Central) { fmt.Println("Disconnect: ", c.ID()) }),
	)

	// A mandatory handler for monitoring device state.
	onStateChanged := func(d gatt.Device, s gatt.State) {
		fmt.Printf("State: %s\n", s)
		switch s {
		case gatt.StatePoweredOn:
			s1 := NewHPSService()
			d.AddService(s1)

			// Advertise device name and service's UUIDs.
			d.AdvertiseNameAndServices("HPS", []gatt.UUID{s1.UUID()})

		default:
		}
	}

	d.Init(onStateChanged)
	select {}
}
