terraform {
  required_providers {
    leifwind = {
      source = "leifwind-io/leifwind"
    }
  }
}

# Delegated/static token (runner path):
provider "leifwind" {
  endpoint = "https://api.leifwind.example"
  token    = var.leifwind_token # or LEIFWIND_TOKEN
}

# Alternative — client_credentials (operator/M2M path):
# provider "leifwind" {
#   endpoint      = "https://api.leifwind.example"
#   issuer        = "https://auth.leifwind.example"   # LEIFWIND_OIDC_ISSUER
#   client_id     = "…"                               # LEIFWIND_CLIENT_ID
#   client_secret = var.client_secret                 # LEIFWIND_CLIENT_SECRET
#   audience      = "326102453042806786"              # LEIFWIND_OIDC_AUDIENCE
# }
