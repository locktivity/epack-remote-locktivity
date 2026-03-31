package locktivity

import "net/http"

func newHTTPTransport() *http.Transport {
	return &http.Transport{}
}
