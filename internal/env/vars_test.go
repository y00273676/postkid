package env

import (
	"reflect"
	"testing"
)

func TestMerge(t *testing.T) {
	tests := []struct {
		name                    string
		request, collection, env map[string]string
		want                    map[string]string
	}{
		{
			name:      "request overrides all",
			request:   map[string]string{"a": "r", "b": "r"},
			collection: map[string]string{"a": "c", "d": "c"},
			env:       map[string]string{"a": "e", "e": "e"},
			want:      map[string]string{"a": "r", "b": "r", "d": "c", "e": "e"},
		},
		{
			name:      "collection overrides env",
			request:   nil,
			collection: map[string]string{"x": "c"},
			env:       map[string]string{"x": "e", "y": "e"},
			want:      map[string]string{"x": "c", "y": "e"},
		},
		{
			name: "all nil",
			want: map[string]string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Merge(tt.request, tt.collection, tt.env)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Merge = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSubstitute(t *testing.T) {
	vars := map[string]string{"base": "https://x.io", "id": "42"}

	tests := []struct {
		name    string
		input   string
		want    string
		missing []string
	}{
		{name: "single", input: "{{base}}/api", want: "https://x.io/api"},
		{name: "multiple", input: "{{base}}/orders/{{id}}", want: "https://x.io/orders/42"},
		{name: "no vars", input: "plain text", want: "plain text"},
		{name: "missing", input: "{{base}}/{{nope}}", want: "https://x.io/{{nope}}", missing: []string{"nope"}},
		{name: "adjacent", input: "{{id}}{{id}}", want: "4242"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, missing := Substitute(tt.input, vars)
			if got != tt.want {
				t.Errorf("result = %q, want %q", got, tt.want)
			}
			if !reflect.DeepEqual(missing, tt.missing) {
				t.Errorf("missing = %v, want %v", missing, tt.missing)
			}
		})
	}
}
