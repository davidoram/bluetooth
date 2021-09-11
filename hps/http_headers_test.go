package hps

import (
	"net/http"
	"reflect"
	"testing"
)

var headerTests = []struct {
	h         http.Header
	truncated bool
}{
	{
		http.Header{
			"Content-Type":   {"text/html; charset=UTF-8"},
			"Content-Length": {"0"},
		},
		false,
	},
	{
		http.Header{
			"Content-Encoding": {"gzip"},
			"Cache-Control":    {" no-cache, no-store, must-revalidate"},
			"Accept":           {"text/html, application/xhtml+xml, application/xml;q=0.9, */*;q=0.8"},
			"X-Forwarded-For":  {"10.125.5.30, 10.125.9.125"},
		},
		false,
	},
	{
		http.Header{},
		false,
	},
}

func TestHeaderEncoding(t *testing.T) {
	for _, tt := range headerTests {
		b, truncated := EncodeHeaders(tt.h)
		if tt.truncated != truncated {
			t.Errorf("got %t, want %t", truncated, tt.truncated)
		}
		h := DecodeHeaders(b)
		if !reflect.DeepEqual(h, tt.h) {
			t.Errorf("got %v, want %v", h, tt.h)
		}

	}
}
