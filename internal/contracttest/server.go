package contracttest

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
)

type Step struct {
	Method   string
	Path     string
	Query    url.Values
	Headers  map[string]string
	Status   int
	JSONBody any
	Body     string
	Check    func(t *testing.T, r *http.Request, body []byte)
}

type Server struct {
	t      *testing.T
	mu     sync.Mutex
	steps  []Step
	cursor int
	server *httptest.Server
}

func NewServer(t *testing.T, steps ...Step) *Server {
	t.Helper()

	s := &Server{
		t:     t,
		steps: steps,
	}

	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("failed to read request body: %v", err)
		}

		step := s.nextStep()

		if r.Method != step.Method {
			t.Fatalf("unexpected method for step %d: expected %s, got %s", s.cursor, step.Method, r.Method)
		}

		if r.URL.Path != step.Path {
			t.Fatalf("unexpected path for step %d: expected %s, got %s", s.cursor, step.Path, r.URL.Path)
		}

		if step.Query != nil && !equalQuery(step.Query, r.URL.Query()) {
			t.Fatalf("unexpected query for step %d: expected %v, got %v", s.cursor, step.Query, r.URL.Query())
		}

		if step.Check != nil {
			step.Check(t, r, body)
		}

		for key, value := range step.Headers {
			w.Header().Set(key, value)
		}
		w.WriteHeader(step.Status)

		switch {
		case step.JSONBody != nil:
			if err := json.NewEncoder(w).Encode(step.JSONBody); err != nil {
				t.Fatalf("failed to encode response body: %v", err)
			}
		case step.Body != "":
			if _, err := io.WriteString(w, step.Body); err != nil {
				t.Fatalf("failed to write response body: %v", err)
			}
		}
	}))

	t.Cleanup(func() {
		s.server.Close()
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.cursor != len(s.steps) {
			t.Fatalf("unused fake server steps: consumed %d of %d", s.cursor, len(s.steps))
		}
	})

	return s
}

func (s *Server) URL() string {
	return s.server.URL
}

func (s *Server) Client() *http.Client {
	return s.server.Client()
}

func (s *Server) nextStep() Step {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cursor >= len(s.steps) {
		s.t.Fatalf("unexpected extra request after %d scripted steps", len(s.steps))
	}

	step := s.steps[s.cursor]
	s.cursor++
	return step
}

func equalQuery(expected, actual url.Values) bool {
	if len(expected) != len(actual) {
		return false
	}

	for key, expectedValues := range expected {
		actualValues, ok := actual[key]
		if !ok || len(actualValues) != len(expectedValues) {
			return false
		}
		for i := range expectedValues {
			if actualValues[i] != expectedValues[i] {
				return false
			}
		}
	}

	return true
}
