package main

/*
 * Peripheral is the server component
 * Accpets HPS requests & calls out to local service
 *
 */

import (
	"encoding/binary"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strings"

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

var (
	uri      string
	headers  string
	body     string
	verb     string
	protocol string

	resp     *http.Response
	respBody []byte
)

func sendRequest() error {
	// Create client
	client := &http.Client{}

	// Create request
	req, err := http.NewRequest(verb, fmt.Sprintf("%s://%s", protocol, uri), strings.NewReader(body))

	// Headers
	if headers != "" {
		for _, h := range strings.Split(headers, "\n") {
			values := strings.Split(h, "=")
			if len(values) != 2 {
				log.Printf("Ignoring invalid header %v", h)
				continue
			}
			log.Printf("Header %s=%s", values[0], values[1])
			req.Header.Add(values[0], values[1])
		}
	}

	// Fetch Request
	log.Printf("Fetching request %v ...", req)
	resp, err = client.Do(req)

	if err != nil {
		log.Println("HTTP call failed: ", err)
		return err
	}

	// Read Response Body
	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Error reading response body: %v", err)
		return err
	}

	// Display Results
	log.Println("response Status  : ", resp.Status)
	log.Println("response Headers : ", resp.Header)
	log.Println("response Body    : ", string(respBody))

	return nil
}

func NewHPSService() *gatt.Service {
	s := gatt.NewService(gatt.MustParseUUID(hps.HpsServiceID))

	// Receive URI
	s.AddCharacteristic(gatt.UUID16(hps.HTTPURIID)).HandleWriteFunc(
		func(r gatt.Request, data []byte) (status byte) {
			uri = string(data)
			log.Println("write URI:", uri)
			return gatt.StatusSuccess
		})

	// Receive Headers
	hc := s.AddCharacteristic(gatt.UUID16(hps.HTTPHeadersID))
	hc.HandleWriteFunc(
		func(r gatt.Request, data []byte) (status byte) {
			headers = string(data)
			log.Println("write headers:", headers)
			return gatt.StatusSuccess
		})
	hc.HandleReadFunc(
		func(rsp gatt.ResponseWriter, req *gatt.ReadRequest) {
			log.Printf("read headers")

			_, err := rsp.Write([]byte(encodeHeaders(resp)))
			if err != nil {
				log.Printf("HTTP Header read err: %v", err)
			}
		})

	// Receive Entity Body
	hb := s.AddCharacteristic(gatt.UUID16(hps.HTTPEntityBodyID))
	hb.HandleWriteFunc(
		func(r gatt.Request, data []byte) (status byte) {
			body = string(data)
			log.Println("write entityBody:", body)
			return gatt.StatusSuccess
		})
	hb.HandleReadFunc(
		func(rsp gatt.ResponseWriter, req *gatt.ReadRequest) {
			log.Println("read entityBody")
			_, err := rsp.Write(respBody)
			if err != nil {
				log.Printf("HTTP body read err: %v", err)
			}
		})

	// Receive control point, this triggers the HTTP request to occur
	s.AddCharacteristic(gatt.UUID16(hps.HTTPControlPointID)).HandleWriteFunc(
		func(r gatt.Request, data []byte) (status byte) {
			log.Println("write controlPoint")

			switch data[0] {
			case hps.HTTPGet, hps.HTTPSGet:
				verb = "GET"
			case hps.HTTPHead, hps.HTTPSHead:
				verb = "HEAD"
			case hps.HTTPPut, hps.HTTPSPut:
				verb = "PUT"
			case hps.HTTPPost, hps.HTTPSPost:
				verb = "POST"
			case hps.HTTPDelete, hps.HTTPSDelete:
				verb = "DELETE"
			}
			log.Println("verb:", verb)

			switch data[0] {
			case hps.HTTPSGet, hps.HTTPSHead, hps.HTTPSPut, hps.HTTPSPost, hps.HTTPSDelete:
				protocol = "https"
			default:
				protocol = "http"
			}
			log.Println("protocol:", protocol)

			// TODO - perform the HTTP request here
			go sendRequest()

			return gatt.StatusSuccess
		})

	s.AddCharacteristic(gatt.UUID16(hps.HTTPStatusCodeID)).HandleNotifyFunc(
		func(r gatt.Request, n gatt.Notifier) {
			log.Println("notify status code")
			var statusCode uint16 = 200
			var dataStatus uint8 = hps.DataStatusHeadersReceived | hps.DataStatusBodyReceived

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

func encodeHeaders(resp *http.Response) string {
	log.Printf("encode headers: %v", resp)
	headers := make([]string, 0)
	for name, values := range resp.Header {
		// Loop over all values for the name.
		for _, value := range values {
			headers = append(headers, fmt.Sprintf("%s=%s", name, value))
		}
	}
	return strings.Join(headers, "\n")
}

func main() {
	flag.Parse()
	log.Printf("Device name: %s", *deviceName)
	d, err := gatt.NewDevice(option.DefaultServerOptions...)
	if err != nil {
		log.Fatalf("Failed to open device, err: %s", err)
	}

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
