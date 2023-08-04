package interpreter

import (
	"gorm.io/gorm"
	"testing"
)

func Test_getCode(t *testing.T) {
	packageName, code, err := GetCode("../plugins/demo.go")
	if err != nil {
		t.Fatal(err)
	}
	t.Log(packageName, code)
}
func Test_parseCode(t *testing.T) {
	packageName, codeStr, err := GetCode("../plugins/demo.go")
	if err != nil {
		t.Fatal(err)
	}
	code, err := NewCode(packageName, codeStr)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(code)
}
func Test_findMethod(t *testing.T) {
	packageName, codeStr, err := GetCode("../plugins/demo.go")
	if err != nil {
		t.Fatal(err)
	}
	code, err := NewCode(packageName, codeStr)
	if err != nil {
		t.Fatal(err)
	}
	fnPtr, err := FindMethod[func(db *gorm.DB) string](code, "HandleTest")
	if err != nil {
		t.Fatal(err)
	}
	if fnPtr == nil {
		t.Fatal("未找到方法")
	}
	t.Log((*fnPtr)(nil))
}
