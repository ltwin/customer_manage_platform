package customer

import (
	"reflect"
	"testing"
)

func TestNormalizeListFilterStatusSet(t *testing.T) {
	tests := []struct {
		name      string
		status    string
		want      string
		wantError bool
	}{
		{name: "default", want: StatusActive},
		{name: "single remains compatible", status: StatusArchived, want: StatusArchived},
		{name: "all remains compatible", status: StatusAll, want: StatusAll},
		{name: "set is canonical", status: " archived,active ", want: "active,archived"},
		{name: "all cannot mix", status: "all,active", wantError: true},
		{name: "duplicates rejected", status: "active,active", wantError: true},
		{name: "unknown rejected", status: "active,unknown", wantError: true},
		{name: "empty member rejected", status: "active,,archived", wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeListFilter(ListFilter{Status: tt.status})
			if tt.wantError {
				if err == nil {
					t.Fatalf("normalize status %q should fail", tt.status)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalize status %q: %v", tt.status, err)
			}
			if got.Status != tt.want {
				t.Fatalf("status: want %q, got %q", tt.want, got.Status)
			}
		})
	}
}

func TestBuildCustomerFilterUsesStatusSetBeforePagination(t *testing.T) {
	filter, err := normalizeListFilter(ListFilter{Status: "active,archived"})
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	condition, args := buildCustomerFilter(filter)
	if condition != "status = ANY($2::text[])" {
		t.Fatalf("condition: %q", condition)
	}
	want := []any{[]string{StatusActive, StatusArchived}}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args: want %#v, got %#v", want, args)
	}
}
