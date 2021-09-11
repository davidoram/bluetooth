package hps

import (
	"fmt"
)

func DecodeURLScheme(b byte) (string, error) {
	switch b {
	case HTTPSGet, HTTPSHead, HTTPSPut, HTTPSPost, HTTPSDelete:
		return "https", nil
	case HTTPGet, HTTPHead, HTTPPut, HTTPPost, HTTPDelete:
		return "http", nil
	default:
		return "", fmt.Errorf("Unable to decode URL Scheme from byte '%v'", b)
	}
}
