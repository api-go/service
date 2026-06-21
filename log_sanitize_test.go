package service

import (
	"reflect"
	"testing"
)

func TestSanitizeScalars(t *testing.T) {
	opts := sanitizeOpts{maxSize: 200, fieldSize: 20, arrayNum: 3, objectNum: 10}

	tests := []struct {
		name     string
		input    any
		expected any
	}{
		{"nil", nil, nil},
		{"int", 42, 42},
		{"float", 3.14, 3.14},
		{"bool_true", true, true},
		{"bool_false", false, false},
		{"string_short", "hello", "hello"},
		{"string_exact", "12345678901234567890", "12345678901234567890"},
		{"string_over", "123456789012345678901234567890", "12345678901234567890"},
		{"string_unicode", "你好世界你好世界你好世界你好世界你好世界你好世界你好世界", "你好世界你好世界你好世界你好世界你好世界"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeLogData(tt.input, opts)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("got %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestSanitizeMapBasic(t *testing.T) {
	opts := sanitizeOpts{maxSize: 200, fieldSize: 20, arrayNum: 3, objectNum: 10}

	input := map[string]any{
		"name":   "John",
		"bio":    "A very long biography that should be truncated at twenty",
		"status": "active",
	}
	expected := map[string]any{
		"name":   "John",
		"bio":    "A very long biograph",
		"status": "active",
	}

	got := sanitizeLogData(input, opts)
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

func TestSanitizeMapObjectNum(t *testing.T) {
	opts := sanitizeOpts{maxSize: 200, fieldSize: 20, arrayNum: 3, objectNum: 3}

	input := map[string]any{
		"k1": "v1",
		"k2": "v2",
		"k3": "v3",
		"k4": "v4",
		"k5": "v5",
	}
	expected := map[string]any{
		"k1": "v1",
		"k2": "v2",
		"k3": "v3",
	}

	got := sanitizeLogData(input, opts)
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

func TestSanitizeSlice(t *testing.T) {
	opts := sanitizeOpts{maxSize: 200, fieldSize: 20, arrayNum: 3, objectNum: 10}

	input := []any{1, 2, 3, 4, 5, 6, 7}
	expected := []any{1, 2, 3}

	got := sanitizeLogData(input, opts)
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

func TestSanitizeSliceWithStrings(t *testing.T) {
	opts := sanitizeOpts{maxSize: 200, fieldSize: 20, arrayNum: 3, objectNum: 10}

	input := []any{
		"short",
		"this string is definitely way too long for twenty characters",
		"ok",
		"this would be fourth but arrayNum is 3",
	}
	expected := []any{
		"short",
		"this string is defin",
		"ok",
	}

	got := sanitizeLogData(input, opts)
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

func TestSanitizeNested(t *testing.T) {
	opts := sanitizeOpts{maxSize: 200, fieldSize: 20, arrayNum: 3, objectNum: 10}

	input := map[string]any{
		"user": map[string]any{
			"name": "John Doe",
			"bio":  "A very long description that should be truncated at twenty chars",
			"tags": []any{"go", "javascript-long-name", "rust", "python", "java", "c++"},
		},
		"score": 100,
	}
	expected := map[string]any{
		"user": map[string]any{
			"name": "John Doe",
			"bio":  "A very long descript",
			"tags": []any{"go", "javascript-long-name", "rust"},
		},
		"score": 100,
	}

	got := sanitizeLogData(input, opts)
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

func TestSanitizeTopLevelArray(t *testing.T) {
	opts := sanitizeOpts{maxSize: 200, fieldSize: 20, arrayNum: 3, objectNum: 10}

	input := []any{
		map[string]any{"id": 1, "name": "Alice"},
		map[string]any{"id": 2, "name": "Bob"},
		map[string]any{"id": 3, "name": "Charlie"},
		map[string]any{"id": 4, "name": "Diana"},
		map[string]any{"id": 5, "name": "Eve"},
	}
	expected := []any{
		map[string]any{"id": 1, "name": "Alice"},
		map[string]any{"id": 2, "name": "Bob"},
		map[string]any{"id": 3, "name": "Charlie"},
	}

	got := sanitizeLogData(input, opts)
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

func TestSanitizeBudgetExhausted(t *testing.T) {
	opts := sanitizeOpts{maxSize: 22, fieldSize: 20, arrayNum: 10, objectNum: 10}

	input := map[string]any{
		"a": "1234567890",
		"b": "abcdefghij",
		"c": "should-be-dropped",
	}
	expected := map[string]any{
		"a": "1234567890",
		"b": "abcdefghij",
	}

	got := sanitizeLogData(input, opts)
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

func TestSanitizeNestedBudgetExhausted(t *testing.T) {
	opts := sanitizeOpts{maxSize: 30, fieldSize: 20, arrayNum: 10, objectNum: 10}

	input := map[string]any{
		"small": "hi",
		"nested": map[string]any{
			"field1": "1234567890",
			"field2": "abcdefghij",
		},
		"after": "no",
	}
	// 按字母序: after(7) → nested(22) → small(被跳过)
	expected := map[string]any{
		"after": "no",
		"nested": map[string]any{
			"field1": "1234567890",
		},
	}

	got := sanitizeLogData(input, opts)
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

func TestSanitizeEmptyContainers(t *testing.T) {
	opts := sanitizeOpts{maxSize: 200, fieldSize: 20, arrayNum: 3, objectNum: 10}

	tests := []struct {
		name     string
		input    any
		expected any
	}{
		{"empty_map", map[string]any{}, map[string]any{}},
		{"empty_slice", []any{}, []any{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeLogData(tt.input, opts)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("got %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestSanitizeMixedSlice(t *testing.T) {
	opts := sanitizeOpts{maxSize: 200, fieldSize: 20, arrayNum: 10, objectNum: 10}

	input := []any{
		"ok",
		42,
		true,
		nil,
		map[string]any{"nested": "value"},
		"this one is too long and will be trimmed to twenty chars",
		nil,
	}
	expected := []any{
		"ok",
		42,
		true,
		nil,
		map[string]any{"nested": "value"},
		"this one is too long",
		nil,
	}

	got := sanitizeLogData(input, opts)
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}
