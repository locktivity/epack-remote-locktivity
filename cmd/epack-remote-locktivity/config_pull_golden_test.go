package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/locktivity/epack-remote-locktivity/internal/locktivity"
	"github.com/locktivity/epack-remote-locktivity/internal/remote"
)

// epack's tests encode and decode the same files, so a field renamed or
// dropped on either side of config.pull fails both.
func readGolden(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "internal", "remote", "testdata", "config_pull", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func assertAnswerMatchesGolden(t *testing.T, answer map[string]any, name string) {
	t.Helper()
	encoded, err := json.Marshal(answer)
	if err != nil {
		t.Fatal(err)
	}
	var got, want any
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(readGolden(t, name), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("answer differs from %s:\n got %s", name, encoded)
	}
}

func TestConfigPullGolden_AFallbackAnswersWhatEpackDecodes(t *testing.T) {
	client := locktivity.NewMockClient()
	client.CreatePipelineFallbackResponse = &locktivity.PipelineFallbackResponse{PipelineBundleResponse: locktivity.PipelineBundleResponse{
		ID:         "0b9c7a52-6f2e-4d1a-9f3b-2c4d5e6f7a8b",
		Name:       "Northwind production",
		ConfigName: "northwind-production",
		Stream:     "northwind/production",
		RunsIn:     "GitLab CI",
		Revision:   2,
		Folder:     "northwind/production",
		Files:      map[string]string{"epack.yaml": "stream: northwind/production\n"},
		Shas:       map[string]string{"epack.yaml": "3f1c2b0e"},
		Lockfile:   "schema_version: 1\n",
		Requirements: &locktivity.BundleRequirements{Epack: "v0.5.0", Hints: []locktivity.BundleRequirementHint{{
			Name: "SENTRY_AUTH_TOKEN",
			Text: "Add it as a masked CI/CD variable in northwind/compliance",
			URL:  "https://gitlab.com/northwind/compliance/-/settings/ci_cd",
		}}},
		Changes:      []string{"Collectors added: Sentry"},
		ChangesSince: 1,
	}}

	answer := processRequest(readGolden(t, "request.json"), remote.NewHandlerWithClient(client, nil))

	call := client.CreatePipelineFallbackCalls
	if len(call) != 1 || call[0].Revision != 3 || call[0].Purpose != "run" || len(call[0].Missing) != 2 || call[0].Missing[1].Kind != "publisher" {
		t.Fatalf("the golden request decoded as %#v", call)
	}
	assertAnswerMatchesGolden(t, answer, "answer.json")
}

func TestConfigPullGolden_NoFallbackAnswersWhatEpackDecodes(t *testing.T) {
	client := locktivity.NewMockClient()
	client.CreatePipelineFallbackResponse = &locktivity.PipelineFallbackResponse{NoFallback: &locktivity.NoFallback{
		Reason: "Every revision before 3 needs SENTRY_AUTH_TOKEN, or runs something a later revision removed.",
	}}

	answer := processRequest(readGolden(t, "request.json"), remote.NewHandlerWithClient(client, nil))

	assertAnswerMatchesGolden(t, answer, "no_fallback.json")
}
