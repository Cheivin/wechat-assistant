package main

import (
	"fmt"
	"testing"
)

func TestTOTPToken(t *testing.T) {
	secret := "MZXW6YTBOI======"
	ts := 1690428301
	fmt.Println(ts)
	token, err := TOTPToken(secret, int64(ts))
	if err != nil {
		t.Fatal(err)
	}
	if "504060" != token {
		t.Error("验证失败")
	} else {
		t.Log("验证通过")
	}
}
