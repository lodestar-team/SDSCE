package session

import (
	"crypto/rand"
	"encoding/base64"
)

// GenerateID generates a cryptographically random session ID.
// Returns a 22-character URL-safe base64 string (128 bits of entropy).
func GenerateID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
