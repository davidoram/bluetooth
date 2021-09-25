package hps

import (
	"fmt"

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
	c.Error = p.WriteCharacteristic(c.uriChr, []byte(urlStr), true)
	if c.Error != nil {
		return c.Error
	}

	c.Error = p.WriteCharacteristic(c.hdrsChr, []byte(c.Request.Headers.String()), true)
	if c.Error != nil {
		return c.Error
	}

	c.Error = p.WriteCharacteristic(c.bodyChr, []byte(c.Request.Body), true)
	if c.Error != nil {
		return c.Error
	}
	var code uint8
	code, c.Error = EncodeMethodScheme(c.Request.Method, c.Request.Url.Scheme)
	if c.Error != nil {
		return c.Error
	}
	c.Error = p.WriteCharacteristic(c.controlChr, []byte{code}, false)
	if c.Error != nil {
		return c.Error
	}

	// Wait for a response
	gotResponse := <-c.ResponseChannel

	if gotResponse {
		c.Response.Body, c.Error = p.ReadCharacteristic(c.bodyChr)
		if c.Error != nil {
			return c.Error
		}

		c.Response.Headers, c.Error = p.ReadCharacteristic(c.hdrsChr)
		if c.Error != nil {
			return c.Error
		}
		log.Info().Str("body", string(c.Response.Body)).
			Interface("headers", c.Response.DecodedHeaders()).
			Bool("headers_received", c.Response.NotifyStatus.HeadersReceived).
			Bool("headers_truncated", c.Response.NotifyStatus.HeadersTruncated).
			Bool("body_received", c.Response.NotifyStatus.BodyReceived).
			Bool("body_truncated", c.Response.NotifyStatus.BodyTruncated).
			Msg("read resoponse")
		return nil
	}
	return fmt.Errorf("Timeout waiting for response")
}
