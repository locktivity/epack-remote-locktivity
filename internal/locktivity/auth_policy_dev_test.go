//go:build dev

package locktivity

import "testing"

func TestIsAllowedAllModeAuthURL_Dev(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{url: "https://app.locktivity.com", want: true},
		{url: "https://app.locktivity.com:443", want: true},
		{url: "http://app.locktivity.com", want: false},
		{url: "https://app.locktivity.com:8443", want: false},
		{url: "http://localhost:3000", want: true},
		{url: "https://foo.localhost", want: true},
		{url: "http://127.0.0.1:3000", want: true},
		{url: "ftp://localhost:3000", want: false},
		{url: "https://evil.example.com", want: false},
	}

	for _, tc := range tests {
		got := IsAllowedAllModeAuthURL(tc.url)
		if got != tc.want {
			t.Fatalf("IsAllowedAllModeAuthURL(%q)=%v want %v", tc.url, got, tc.want)
		}
	}
}
