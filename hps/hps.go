package hps

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

const (
	PeripheralID = "b4a77f05-2524-4330-bcbb-5aafd2a9329b"
	DeviceName   = "davidoram/HPS"
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

	HeadersReceived  uint8 = 0x01
	HeadersTruncated uint8 = 0x02
	BodyReceived     uint8 = 0x04
	BodyTruncated    uint8 = 0x08

	DataStatusHeadersReceived  uint8 = 0x01
	DataStatusHeadersTruncated uint8 = 0x02
	DataStatusBodyReceived     uint8 = 0x04
	DataStatusBodyTruncated    uint8 = 0x08
)

type NotifyStatus struct {
	HeadersReceived  bool
	HeadersTruncated bool
	BodyReceived     bool
	BodyTruncated    bool
	StatusCode       int
}

func (n NotifyStatus) Encode() []byte {
	var dataStatus uint8
	if n.BodyReceived {
		dataStatus = dataStatus | BodyReceived
	}
	if n.BodyTruncated {
		dataStatus = dataStatus | BodyTruncated
	}
	if n.HeadersReceived {
		dataStatus = dataStatus | HeadersReceived
	}
	if n.HeadersTruncated {
		dataStatus = dataStatus | HeadersTruncated
	}

	// Http Status code eg: 200
	var sc uint16 = uint16(n.StatusCode)

	var b bytes.Buffer
	binary.Write(&b, binary.LittleEndian, sc)
	binary.Write(&b, binary.LittleEndian, dataStatus)
	return b.Bytes()
}

func DecodeNotifyStatus(buf []byte) (NotifyStatus, error) {

	var ns NotifyStatus
	r := bytes.NewReader(buf)

	var data struct {
		StatusCode uint16
		DataStatus uint8
	}

	if err := binary.Read(r, binary.LittleEndian, &data); err != nil {
		fmt.Println("binary.Read failed:", err)
		return ns, err
	}
	ns.HeadersReceived = data.DataStatus&HeadersReceived == HeadersReceived
	ns.HeadersTruncated = data.DataStatus&HeadersTruncated == HeadersTruncated
	ns.BodyReceived = data.DataStatus&BodyReceived == BodyReceived
	ns.BodyTruncated = data.DataStatus&BodyTruncated == BodyTruncated

	// Http Status code eg: 200
	ns.StatusCode = int(data.StatusCode)

	return ns, nil
}
