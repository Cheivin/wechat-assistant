package main

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"time"
)

func TOTPToken(secret string, timestamp uint64) (string, error) {
	key := make([]byte, base32.StdEncoding.DecodedLen(len(secret)))
	_, err := base32.StdEncoding.Decode(key, []byte(secret))
	if err != nil {
		return "", err
	}
	message := make([]byte, 8)
	binary.BigEndian.PutUint64(message, timestamp/30)
	hmacSha1 := hmac.New(sha1.New, key)
	hmacSha1.Write(message)
	hash := hmacSha1.Sum([]byte{})
	offset := hash[len(hash)-1] & 0xF
	truncatedHash := hash[offset : offset+4]
	return fmt.Sprintf("%06d", (binary.BigEndian.Uint32(truncatedHash)&0x7FFFFFFF)%1000000), nil
}

func TOTPVerify(secret string, offsetSize int, code string) bool {
	timestamp := uint64(time.Now().Unix())
	if offsetSize == 0 {
		verifyCode, _ := TOTPToken(secret, timestamp)
		return code == verifyCode
	}
	for i := -offsetSize; i <= offsetSize; i++ {
		offset := uint64(i)
		if verifyCode, _ := TOTPToken(secret, (timestamp+offset)*offset); verifyCode == code {
			return true
		}
	}
	return false
}
