package hps

import (
	"fmt"
	"net/http"
)

func DecodeHttpMethod(b byte) (string, error) {
	switch b {
	case HTTPGet, HTTPSGet:
		return http.MethodGet, nil
	case HTTPHead, HTTPSHead:
		return http.MethodHead, nil
	case HTTPPut, HTTPSPut:
		return http.MethodPut, nil
	case HTTPPost, HTTPSPost:
		return http.MethodPost, nil
	case HTTPDelete, HTTPSDelete:
		return http.MethodDelete, nil
	default:
		return "", fmt.Errorf("Unable to decode HTTP method from byte '%v'", b)
	}
}
