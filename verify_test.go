package service

import (
	"testing"
)

type TestUser struct {
	Name string `verify:"length:2-10"`
	Age  int    `verify:"between:18-100"`
	Type string `verify:"in:admin,user,guest"`
}

type NestedStruct struct {
	TestUser
	Note string `verify:"^.{1,20}$"`
}

func TestVerifyStruct(t *testing.T) {
	u := TestUser{Name: "Star", Age: 25, Type: "admin"}
	if ok, f := VerifyStruct(u, nil); !ok {
		t.Errorf("VerifyStruct failed on valid user, field: %s", f)
	}

	u.Name = "S"
	if ok, f := VerifyStruct(u, nil); ok || f != "name" {
		t.Errorf("VerifyStruct should fail on short name, got ok=%v, field=%s", ok, f)
	}

	u.Name = "Star"
	u.Age = 10
	if ok, f := VerifyStruct(u, nil); ok || f != "age" {
		t.Errorf("VerifyStruct should fail on young age, got ok=%v, field=%s", ok, f)
	}

	u.Age = 25
	u.Type = "invalid"
	if ok, f := VerifyStruct(u, nil); ok || f != "type" {
		t.Errorf("VerifyStruct should fail on invalid type, got ok=%v, field=%s", ok, f)
	}
}

func TestNestedVerify(t *testing.T) {
	n := NestedStruct{
		TestUser: TestUser{Name: "Star", Age: 25, Type: "user"},
		Note:     "Hello",
	}
	if ok, f := VerifyStruct(n, nil); !ok {
		t.Errorf("Nested VerifyStruct failed on valid data, field: %s", f)
	}

	n.TestUser.Age = 5
	if ok, f := VerifyStruct(n, nil); ok || f != "age" {
		t.Errorf("Nested VerifyStruct should fail on nested age, got ok=%v, field=%s", ok, f)
	}
}

func TestCustomVerify(t *testing.T) {
	RegisterVerifyFunc("odd", func(in any, args []string) bool {
		val := in.(int)
		return val%2 != 0
	})

	type OddStruct struct {
		Num int `verify:"odd"`
	}

	o := OddStruct{Num: 3}
	if ok, f := VerifyStruct(o, nil); !ok {
		t.Errorf("Custom verify failed on odd number, field: %s", f)
	}

	o.Num = 4
	if ok, f := VerifyStruct(o, nil); ok || f != "num" {
		t.Errorf("Custom verify should fail on even number, got ok=%v, field=%s", ok, f)
	}
}
