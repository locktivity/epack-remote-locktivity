package locktivity

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestRequestsNameTheMachine(t *testing.T) {
	var got string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get(MachineHeader)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"pipe_1","name":"n","config_name":"n","stream":"s","revision":1,"files":{},"shas":{}}`))
	}))
	defer server.Close()

	client := NewClientWithHTTP(server.Client(), server.URL)
	if _, err := client.GetPipelineBundle(t.Context(), "n"); err != nil {
		t.Fatalf("GetPipelineBundle: %v", err)
	}
	host, _ := os.Hostname()
	if host == "" {
		t.Skip("no hostname on this machine")
	}
	if got == "" || len(got) > machineNameMaxLength {
		t.Fatalf("%s = %q", MachineHeader, got)
	}
}
