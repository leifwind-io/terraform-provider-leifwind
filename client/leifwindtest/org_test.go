// SPDX-License-Identifier: MPL-2.0

package leifwindtest

import (
	"strings"
	"testing"
)

func TestNewOrgMintsJWTWithOrgAndAudience(t *testing.T) {
	s := sharedStack(t)
	org := s.NewOrg(t)
	tok := org.Token(t, s)
	if strings.Count(tok, ".") != 2 {
		t.Fatalf("expected a JWT (3 segments), got %q…", tok[:min(len(tok), 20)])
	}
	claims := DecodeClaims(t, tok)
	if claims["urn:zitadel:iam:user:resourceowner:id"] != org.ID {
		t.Fatalf("resourceowner claim = %v, want %s", claims["urn:zitadel:iam:user:resourceowner:id"], org.ID)
	}
	aud, _ := claims["aud"].([]any)
	found := false
	for _, a := range aud {
		if a == s.Audience {
			found = true
		}
	}
	if !found {
		t.Fatalf("audience %s not in aud %v", s.Audience, aud)
	}
}
