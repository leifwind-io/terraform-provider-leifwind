// SPDX-License-Identifier: MPL-2.0

package provider

// RawConfig is the provider configuration before env-fallback resolution.
// nil = attribute not set in HCL.
type RawConfig struct {
	Endpoint     *string
	Token        *string
	Issuer       *string
	ClientID     *string
	ClientSecret *string
	Audience     *string
}

// Resolved is the effective configuration after env merging.
type Resolved struct {
	Endpoint     string
	Token        string
	Issuer       string
	ClientID     string
	ClientSecret string
	Audience     string
	UseM2M       bool
}

func pick(attr *string, envName string, getenv func(string) string) string {
	if attr != nil && *attr != "" {
		return *attr
	}
	return getenv(envName)
}

// resolveConfig merges attributes with LEIFWIND_* env fallbacks and
// validates: endpoint required; token XOR complete client_credentials trio.
func resolveConfig(raw RawConfig, getenv func(string) string) (Resolved, []string) {
	r := Resolved{
		Endpoint:     pick(raw.Endpoint, "LEIFWIND_ENDPOINT", getenv),
		Token:        pick(raw.Token, "LEIFWIND_TOKEN", getenv),
		Issuer:       pick(raw.Issuer, "LEIFWIND_OIDC_ISSUER", getenv),
		ClientID:     pick(raw.ClientID, "LEIFWIND_CLIENT_ID", getenv),
		ClientSecret: pick(raw.ClientSecret, "LEIFWIND_CLIENT_SECRET", getenv),
		Audience:     pick(raw.Audience, "LEIFWIND_OIDC_AUDIENCE", getenv),
	}
	var errs []string
	if r.Endpoint == "" {
		errs = append(errs, "endpoint is required: set the endpoint attribute or LEIFWIND_ENDPOINT")
	}
	anyM2M := r.Issuer != "" || r.ClientID != "" || r.ClientSecret != ""
	switch {
	case r.Token != "" && anyM2M:
		errs = append(errs, "token and issuer/client_id/client_secret are mutually exclusive: configure either a static token or client_credentials, not both")
	case r.Token != "":
		// static token path — ok
	case anyM2M:
		missing := ""
		if r.Issuer == "" {
			missing += " issuer (LEIFWIND_OIDC_ISSUER)"
		}
		if r.ClientID == "" {
			missing += " client_id (LEIFWIND_CLIENT_ID)"
		}
		if r.ClientSecret == "" {
			missing += " client_secret (LEIFWIND_CLIENT_SECRET)"
		}
		if missing != "" {
			errs = append(errs, "incomplete client_credentials configuration, missing:"+missing)
		} else {
			r.UseM2M = true
		}
	default:
		errs = append(errs, "no credentials: set token (LEIFWIND_TOKEN) or issuer/client_id/client_secret (LEIFWIND_OIDC_ISSUER, LEIFWIND_CLIENT_ID, LEIFWIND_CLIENT_SECRET)")
	}
	return r, errs
}
