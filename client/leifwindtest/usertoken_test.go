// SPDX-License-Identifier: MPL-2.0

package leifwindtest

import "testing"

func TestUserTokenIsDelegatedUserShaped(t *testing.T) {
	s := sharedStack(t)
	org := s.NewOrg(t)
	tok := s.UserToken(t, org)
	claims := DecodeClaims(t, tok)
	if claims["email"] == nil {
		t.Fatalf("delegated token must carry email claim, got claims: %v", claims)
	}
	if claims["urn:zitadel:iam:user:resourceowner:id"] != org.ID {
		t.Fatalf("wrong org claim: %v", claims["urn:zitadel:iam:user:resourceowner:id"])
	}
	if sub, _ := claims["sub"].(string); sub == org.MachineUserID {
		t.Fatal("sub must be the human user, not the machine actor")
	}
	if claims["act"] == nil {
		t.Log("note: no act claim present — acceptable, but check ZITADEL version behavior")
	}
}

func TestForgedTokenHasValidShape(t *testing.T) {
	s := sharedStack(t)
	org := s.NewOrg(t)
	tok := s.ForgedToken(t, org)
	claims := DecodeClaims(t, tok)
	if claims["iss"] != s.Issuer {
		t.Fatalf("forged token should carry the real issuer, got %v", claims["iss"])
	}
}

// TestUserTokenTwiceSameOrg: UserToken must be idempotent per Org (LW-110).
// The second call re-grants ORG_END_USER_IMPERSONATOR to the same machine
// user; ZITADEL answers 409 AlreadyExists, which must be tolerated.
func TestUserTokenTwiceSameOrg(t *testing.T) {
	s := sharedStack(t)
	org := s.NewOrg(t)
	for i := 1; i <= 2; i++ {
		tok := s.UserToken(t, org)
		claims := DecodeClaims(t, tok)
		if claims["email"] == nil {
			t.Fatalf("call %d: delegated token must carry email claim, got claims: %v", i, claims)
		}
		if claims["urn:zitadel:iam:user:resourceowner:id"] != org.ID {
			t.Fatalf("call %d: wrong org claim: %v", i, claims["urn:zitadel:iam:user:resourceowner:id"])
		}
	}
}
