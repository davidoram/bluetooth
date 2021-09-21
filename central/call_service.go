package main

import (
	"fmt"
	"time"

	"github.com/davidoram/bluetooth/hps"
	"github.com/paypal/gatt"

	"github.com/rs/zerolog/log"
)

// CallService makes the remote call, and returns the response or an error
func (c *Connection) CallService(p gatt.Peripheral) error {
	defer p.Device().CancelConnection(p)

	log.Info().Str("uri", c.Request.Url.String()).
		Interface("headers", c.Request.Headers).
		Str("body", c.Request.Body).
		Str("method", c.Request.Method).
		Str("schema", c.Request.Url.Scheme).
		Msg("call service")

	urlStr := fmt.Sprintf("%s%s", c.Request.Url.Host, c.Request.Url.EscapedPath())
	c.Error = p.WriteCharacteristic(uriChr, []byte(urlStr), true)
	if c.Error != nil {
		return c.Error
	}

	c.Error = p.WriteCharacteristic(hdrsChr, []byte(c.Request.Headers.String()), true)
	if c.Error != nil {
		return c.Error
	}

	c.Error = p.WriteCharacteristic(bodyChr, []byte(c.Request.Body), true)
	if c.Error != nil {
		return c.Error
	}
	var code uint8
	code, c.Error = hps.EncodeMethodScheme(c.Request.Method, c.Request.Url.Scheme)
	if c.Error != nil {
		return c.Error
	}
	c.Error = p.WriteCharacteristic(controlChr, []byte{code}, false)
	if c.Error != nil {
		return c.Error
	}

	log.Info().Dur("timeout", c.Timeout).Msg("waiting for notification")
	time.AfterFunc(c.Timeout, func() {
		log.Warn().Msg("timeout expired, no notification received")
		c.ResponseChannel <- false
	})
	gotResponse := <-c.ResponseChannel
	if gotResponse {
		response.Body, c.Error = p.ReadCharacteristic(bodyChr)
		if c.Error != nil {
			return c.Error
		}

		response.Headers, c.Error = p.ReadCharacteristic(hdrsChr)
		if c.Error != nil {
			return c.Error
		}
		log.Info().Str("body", string(response.Body)).
			Interface("headers", response.DecodedHeaders()).
			Bool("headers_received", response.NotifyStatus.HeadersReceived).
			Bool("headers_truncated", response.NotifyStatus.HeadersTruncated).
			Bool("body_received", response.NotifyStatus.BodyReceived).
			Bool("body_truncated", response.NotifyStatus.BodyTruncated).
			Msg("read resoponse")
		return nil
	}
	return fmt.Errorf("Timeout waiting for response")
}
