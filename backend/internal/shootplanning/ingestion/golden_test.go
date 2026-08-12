package ingestion

import (
	"encoding/json"
	"os"
	"testing"
)

func TestParserGoldenCorpus(t *testing.T) {
	type goldenCase struct {
		Name           string `json:"name"`
		Source         string `json:"source"`
		Kind           string `json:"kind"`
		Normalized     string `json:"normalized"`
		Reference      string `json:"reference"`
		ContentCount   int    `json:"content_count"`
		ReferenceCount int    `json:"reference_count"`
	}
	data, err := os.ReadFile("testdata/parser_v1_corpus.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []goldenCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			out, err := Parse(ParseInput{SessionID: "golden_" + tc.Name, FirstSeenSessionRevision: 1, SourceText: tc.Source})
			if err != nil {
				t.Fatal(err)
			}
			if len(out.ContentCandidates) != tc.ContentCount || len(out.ReferenceLinkCandidates) != tc.ReferenceCount {
				t.Fatalf("counts mismatch: content=%d references=%d", len(out.ContentCandidates), len(out.ReferenceLinkCandidates))
			}
			if tc.Kind != "" && (out.ContentCandidates[0].Kind != tc.Kind || out.ContentCandidates[0].NormalizedContent != tc.Normalized) {
				t.Fatalf("content mismatch: %#v", out.ContentCandidates[0])
			}
			if tc.Reference != "" && out.ReferenceLinkCandidates[0].RawURL != tc.Reference {
				t.Fatalf("reference mismatch: %#v", out.ReferenceLinkCandidates[0])
			}
		})
	}
}
