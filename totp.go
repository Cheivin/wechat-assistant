package main

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"time"
)

func TOTPToken(secret string, timestamp int64) (string, error) {
	return totpToken(secret, 30, timestamp)
}

func totpToken(secret string, offsetSize int, timestamp int64) (string, error) {
	key := make([]byte, base32.StdEncoding.DecodedLen(len(secret)))
	_, err := base32.StdEncoding.Decode(key, []byte(secret))
	if err != nil {
		return "", err
	}

	message := make([]byte, 8)
	binary.BigEndian.PutUint64(message, uint64(timestamp/int64(offsetSize)))
	hmacSha1 := hmac.New(sha1.New, key)
	hmacSha1.Write(message)
	hash := hmacSha1.Sum([]byte{})
	offset := hash[len(hash)-1] & 0xF
	truncatedHash := hash[offset : offset+4]
	return fmt.Sprintf("%06d", (binary.BigEndian.Uint32(truncatedHash)&0x7FFFFFFF)%1000000), nil
}

func TOTPVerify(secret string, offsetSize int, code string) bool {
	timestamp := time.Now().Unix()
	for i := -offsetSize; i <= offsetSize; i++ {
		if verifyCode, _ := totpToken(secret, offsetSize, timestamp+int64(i)); verifyCode == code {
			return true
		}
	}
	return false
}
