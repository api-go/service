package service

import (
	"net/http/httptest"
	"testing"
)

func TestResponseWriteBytes(t *testing.T) {
	recorder := httptest.NewRecorder()
	response := NewResponse(recorder, nil)
	want := "data: 第一条\n\n"
	input := make([]any, len([]byte(want)))
	for i, value := range []byte(want) {
		input[i] = int64(value)
	}
	if _, err := response.WriteBytes(input); err != nil {
		t.Fatal(err)
	}
	if got := recorder.Body.String(); got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func TestResponseWriteBypassesOutputFilterBuffer(t *testing.T) {
	recorder := httptest.NewRecorder()
	response := NewResponse(recorder, &WebServer{hasOutFilter: true})
	if _, err := response.Write([]byte("stream")); err != nil {
		t.Fatal(err)
	}
	if recorder.Body.String() != "stream" || response.filteredWrite {
		t.Fatalf("direct write was buffered: wire=%q buffer=%q", recorder.Body.String(), response.body)
	}
}

func TestResponseWriteFilteredOptsIntoOutputFilterBuffer(t *testing.T) {
	recorder := httptest.NewRecorder()
	response := NewResponse(recorder, &WebServer{hasOutFilter: true})
	if _, err := response.WriteFiltered([]byte("html")); err != nil {
		t.Fatal(err)
	}
	if recorder.Body.Len() != 0 || !response.filteredWrite || string(response.body) != "html" {
		t.Fatalf("filtered write was not buffered: wire=%q buffer=%q", recorder.Body.String(), response.body)
	}
}
