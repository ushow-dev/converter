package auth

import (
	"crypto/rand"
	"fmt"
)

func generateID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}
