// Package userdb handles Unix user creation, NT-hash computation, and
// population of Samba's passdb.
package userdb

import (
	"encoding/hex"
	"fmt"
	"strings"
	"unicode/utf16"

	"golang.org/x/crypto/md4"
)

// NTHashPrefix marks a password value as a pre-computed NT hash rather than
// plaintext. Format: "$NT$<32-uppercase-hex>".
const NTHashPrefix = "$NT$"

// NTHash computes the NT password hash: MD4 of the UTF-16LE encoding of the
// plaintext. Returns 32 uppercase hex characters.
func NTHash(plaintext string) string {
	utf16le := utf16.Encode([]rune(plaintext))
	buf := make([]byte, len(utf16le)*2)
	for i, r := range utf16le {
		buf[i*2] = byte(r)
		buf[i*2+1] = byte(r >> 8)
	}
	h := md4.New()
	h.Write(buf)
	return strings.ToUpper(hex.EncodeToString(h.Sum(nil)))
}

// NormalizeHash returns the NT hash for a passwords.yaml entry. Plaintext is
// hashed; "$NT$..." is validated and returned verbatim (uppercased).
func NormalizeHash(entry string) (string, error) {
	if !strings.HasPrefix(entry, NTHashPrefix) {
		return NTHash(entry), nil
	}
	h := strings.ToUpper(strings.TrimPrefix(entry, NTHashPrefix))
	if len(h) != 32 {
		return "", fmt.Errorf("NT hash must be 32 hex chars, got %d", len(h))
	}
	if _, err := hex.DecodeString(h); err != nil {
		return "", fmt.Errorf("NT hash not valid hex: %w", err)
	}
	return h, nil
}
