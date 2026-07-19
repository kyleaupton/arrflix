// Package token mints single-use, link-delivered secrets — invite links today,
// password-reset links next. A raw token is handed to the user (inside a URL) and
// never stored; only its sha256 hash is persisted, so a database read can't
// reconstruct a usable token. This mirrors the session refresh-token posture in
// internal/service/session.go, factored out so every link-based flow shares it.
package token

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
)

// Generate returns a fresh high-entropy token and its sha256 hash. Store the hash;
// deliver the raw token in the link.
func Generate() (raw string, hash []byte, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", nil, err
	}
	raw = base64.RawURLEncoding.EncodeToString(buf)
	return raw, Hash(raw), nil
}

// Hash returns the sha256 of a raw token, for storage and for lookup on redemption.
func Hash(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}

// Equal reports whether a raw token matches a stored hash, in constant time.
func Equal(raw string, hash []byte) bool {
	return subtle.ConstantTimeCompare(Hash(raw), hash) == 1
}
