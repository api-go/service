package service

import (
	"testing"
)

func TestResult(t *testing.T) {
	r := &Result{}
	r.OK()
	if !r.Ok {
		t.Error("Result.OK() failed")
	}

	r.Failed("error", Argot("ERR_CODE"))
	if r.Ok || r.Message != "error" || r.Argot != "ERR_CODE" {
		t.Error("Result.Failed() failed")
	}

	r.Done(true, "never")
	if !r.Ok {
		t.Error("Result.Done(true) failed")
	}

	r.Done(false, "failed", Argot("FAIL"))
	if r.Ok || r.Message != "failed" || r.Argot != "FAIL" {
		t.Error("Result.Done(false) failed")
	}
}

func TestCodeResult(t *testing.T) {
	cr := &CodeResult{}
	cr.OK()
	if cr.Code != 1 {
		t.Error("CodeResult.OK() failed")
	}

	cr.Failed(500, "internal error")
	if cr.Code != 500 || cr.Message != "internal error" {
		t.Error("CodeResult.Failed() failed")
	}
}
