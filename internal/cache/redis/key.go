package cache

import (
	"crypto/sha256"
	"encoding/hex"
)

func Key(method, rawURL string) string {
	sum := sha256.Sum256([]byte(method + "\n" + rawURL))
	return hex.EncodeToString(sum[:])
}
