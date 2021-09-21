package hps

import "net/url"

type Request struct {
	Url     url.URL
	Headers ArrayStr
	Body    string
	Method  string
}
