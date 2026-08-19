package shootplanning

import (
	"encoding/json"
	"testing"
)

func TestOptionalUnmarshalJSONPreservesPatchTriState(t *testing.T) {
	t.Run("omitted", func(t *testing.T) {
		var body struct {
			Value Optional[string] `json:"value"`
		}
		if err := json.Unmarshal([]byte(`{}`), &body); err != nil {
			t.Fatal(err)
		}
		if body.Value.Specified || body.Value.Null || body.Value.Value != "" {
			t.Fatalf("omitted field changed state: %+v", body.Value)
		}
	})

	t.Run("null", func(t *testing.T) {
		var body struct {
			Value Optional[string] `json:"value"`
		}
		if err := json.Unmarshal([]byte(`{"value":null}`), &body); err != nil {
			t.Fatal(err)
		}
		if !body.Value.Specified || !body.Value.Null || body.Value.Value != "" {
			t.Fatalf("null field lost state: %+v", body.Value)
		}
	})

	t.Run("value", func(t *testing.T) {
		var body struct {
			Value Optional[string] `json:"value"`
		}
		if err := json.Unmarshal([]byte(`{"value":"notes"}`), &body); err != nil {
			t.Fatal(err)
		}
		if !body.Value.Specified || body.Value.Null || body.Value.Value != "notes" {
			t.Fatalf("value field lost state: %+v", body.Value)
		}
	})

	t.Run("wrong type", func(t *testing.T) {
		var body struct {
			Value Optional[string] `json:"value"`
		}
		if err := json.Unmarshal([]byte(`{"value":42}`), &body); err == nil {
			t.Fatal("numeric value decoded as string")
		}
	})
}
