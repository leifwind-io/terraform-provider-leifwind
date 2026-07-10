// SPDX-License-Identifier: MPL-2.0

package leifwindtest

import (
	"crypto/rand"
	"crypto/rsa"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ForgedToken returns an RS256 JWT with fully correct claims signed by a
// key that is NOT in ZITADEL's JWKS. The backend must reject it (401):
// mirrors the backend's own test_locally_forged_token_rejected.
func (s *Stack) ForgedToken(t testing.TB, org *Org) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	claims := jwt.MapClaims{
		"iss":                                   s.Issuer,
		"aud":                                   []string{s.Audience},
		"sub":                                   "forged-user",
		"iat":                                   now.Unix(),
		"exp":                                   now.Add(5 * time.Minute).Unix(),
		"urn:zitadel:iam:user:resourceowner:id": org.ID,
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = "forged-kid"
	signed, err := tok.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}
