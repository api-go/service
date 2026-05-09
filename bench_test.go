package service_test

import (
	"apigo.cc/go/service"
	"net/http"
	"net/http/httptest"
	"testing"
)

type BenchIn struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

func BenchmarkRouting(b *testing.B) {
	service.Host("*").ANY("/bench", func(in BenchIn) string {
		return "hello " + in.Name
	}).Memo("bench").NoLog200()

	handler := &service.RouteHandler{}
	req, _ := http.NewRequest("GET", "/bench?name=test&age=20", nil)
	w := httptest.NewRecorder()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		handler.ServeHTTP(w, req)
	}
}
