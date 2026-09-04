package service

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientKeyHeadersSynchronizeCookies(t *testing.T) {
	ws := NewWebServer()
	ws.SetClientKeys("Device-ID", "", "Session-ID")
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Session-ID", "authenticated-session")
	req.Header.Set("Device-ID", "current-device")
	req.AddCookie(&http.Cookie{Name: "Session-ID", Value: "stale-session"})
	req.AddCookie(&http.Cookie{Name: "Device-ID", Value: "stale-device"})
	recorder := httptest.NewRecorder()

	ws.handleClientKeys(NewRequest(req), NewResponse(recorder, ws))

	cookies := recorder.Result().Cookies()
	want := map[string]string{"Session-ID": "authenticated-session", "Device-ID": "current-device"}
	for _, cookie := range cookies {
		if value, ok := want[cookie.Name]; ok {
			if cookie.Value != value || cookie.Path != "/" || !cookie.HttpOnly {
				t.Fatalf("unexpected synchronized cookie: %#v", cookie)
			}
			delete(want, cookie.Name)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing synchronized cookies: %#v", want)
	}
}

func TestClientKeyHeadersDoNotRewriteMatchingCookies(t *testing.T) {
	ws := NewWebServer()
	ws.SetClientKeys("Device-ID", "", "Session-ID")
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Session-ID", "same-session")
	req.Header.Set("Device-ID", "same-device")
	req.AddCookie(&http.Cookie{Name: "Session-ID", Value: "same-session"})
	req.AddCookie(&http.Cookie{Name: "Device-ID", Value: "same-device"})
	recorder := httptest.NewRecorder()

	ws.handleClientKeys(NewRequest(req), NewResponse(recorder, ws))

	if cookies := recorder.Result().Cookies(); len(cookies) != 0 {
		t.Fatalf("matching client keys unexpectedly rewrote cookies: %#v", cookies)
	}
}
