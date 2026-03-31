package auth

import "net/http"

func newHTTPTransport() *http.Transport {
	return &http.Transport{}
}
