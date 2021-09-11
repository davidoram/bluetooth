package hps

import (
	"bytes"
	"log"
	"net/http"
	"strings"
)

// EncodeHeaders returns the HTTP Headers from the response, encoded into a
// Byte buffer. The buffer will not exceed the maximum size of HeaderMaxOctets
// Returns the buffer, along with a flag set true if the headers were truncated to fit the
// buffer. Truncation occurs at the end of each Header
func EncodeHeaders(headers http.Header) ([]byte, bool) {
	log.Printf("encode headers: %v", headers)
	truncated := false
	var b bytes.Buffer
	idx := 0
	for name, values := range headers {
		var s strings.Builder
		if idx > 0 {
			s.WriteString("\n")
		}
		idx++
		s.WriteString(name)
		s.WriteString("=")
		// If a header has >1 value, separate them by ", "
		for i, value := range values {
			if i == 0 {
				s.WriteString(value)
			} else {
				s.WriteString(", ")
				s.WriteString(value)
			}
		}

		// Bail if we are going to exceed the maximum size
		if s.Len()+b.Len() > HeaderMaxOctets {
			truncated = true
			break
		}
		b.WriteString(s.String())
	}
	return b.Bytes(), truncated
}

// DecodeHeaders decodes byte[] to http.Header
func DecodeHeaders(b []byte) http.Header {
	headers := http.Header{}
	if len(b) == 0 {
		return headers
	}
	raw := strings.Split(string(b), "\n")
	for _, hdr := range raw {
		// Split into "{key}={values}"
		headerRaw := strings.SplitN(hdr, "=", 2)
		if len(headerRaw) == 2 {
			headers.Add(headerRaw[0], headerRaw[1])
		} else {
			headers.Add(headerRaw[0], "")
		}
	}
	return headers
}
