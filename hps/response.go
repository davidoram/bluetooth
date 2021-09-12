package hps

import "net/http"

type Response struct {
	NotifyStatus NotifyStatus
	Headers      []byte
	Body         []byte
	Notified     bool
}

func (r *Response) DecodedHeaders() http.Header {
	return DecodeHeaders(r.Headers)
}
