package cli

import (
	"strings"
	"testing"
)

func TestCompactJSONSimpleObject(t *testing.T) {
	result := compactJSON(map[string]interface{}{
		"name":    "yeet",
		"version": 1.0,
	}, 0, 4)
	if !strings.Contains(result, "name") {
		t.Errorf("expected 'name' key, got: %s", result)
	}
	if !strings.Contains(result, "yeet") {
		t.Errorf("expected 'yeet' value, got: %s", result)
	}
}

func TestCompactJSONInlineSmallMap(t *testing.T) {
	// Small map with simple values should render inline when nested
	result := compactJSON(map[string]interface{}{
		"parent": map[string]interface{}{"a": 1.0, "b": "x"},
	}, 0, 4)
	if !strings.Contains(result, "{ a: 1, b: \"x\" }") {
		t.Errorf("expected inline nested object, got: %s", result)
	}
}

func TestCompactJSONInlineArray(t *testing.T) {
	// Small simple array renders inline
	result := compactJSON([]interface{}{1.0, 2.0, 3.0}, 0, 4)
	if !strings.Contains(result, "[1, 2, 3]") {
		t.Errorf("expected '[1, 2, 3]', got: %s", result)
	}
}

func TestCompactJSONLargeArray(t *testing.T) {
	arr := make([]interface{}, 10)
	for i := range arr {
		arr[i] = float64(i)
	}
	result := compactJSON(arr, 0, 4)
	if !strings.Contains(result, "+9 more") {
		t.Errorf("expected '+9 more' truncation, got: %s", result)
	}
}

func TestCompactJSONLongString(t *testing.T) {
	long := strings.Repeat("x", 100)
	result := compactJSON(long, 0, 4)
	if !strings.Contains(result, "...") {
		t.Errorf("expected truncation of long string, got: %s", result)
	}
}

func TestCompactJSONSchemaTypes(t *testing.T) {
	obj := map[string]interface{}{
		"name":   "alice",
		"age":    30.0,
		"active": true,
	}
	result := compactJSONSchema(obj, 0, 4)
	if !strings.Contains(result, "string") {
		t.Errorf("expected 'string' type, got: %s", result)
	}
	if !strings.Contains(result, "number") {
		t.Errorf("expected 'number' type, got: %s", result)
	}
	if !strings.Contains(result, "bool") {
		t.Errorf("expected 'bool' type, got: %s", result)
	}
}

func TestIsInlineValue(t *testing.T) {
	cases := []struct {
		v    interface{}
		want bool
	}{
		{"hello", true},
		{42.0, true},
		{true, true},
		{nil, true},
		{[]interface{}{1.0, 2.0, 3.0}, true},                           // simple array ≤5
		{[]interface{}{1.0, 2.0, 3.0, 4.0, 5.0, 6.0}, false},          // too long
		{map[string]interface{}{"a": 1.0, "b": "x"}, true},             // small simple map
		{map[string]interface{}{"a": map[string]interface{}{}}, false},  // nested non-simple
	}
	for _, c := range cases {
		got := isInlineValue(c.v)
		if got != c.want {
			t.Errorf("isInlineValue(%v) = %v, want %v", c.v, got, c.want)
		}
	}
}
