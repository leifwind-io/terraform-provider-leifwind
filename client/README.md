# leifwind Go client

Standalone client for the leifwind metadata API — no Terraform required.

`gitlab.com/leifwind/stream/terraform-provider-leifwind/client` is a
first-class public deliverable in its own right: it is importable
independently of the Terraform provider that lives alongside it in this
repository, has **zero `terraform-plugin-*` dependencies**, and is tagged and
released on its own `client/vX.Y.Z` stream so consumers can pin it without
pulling in any Terraform tooling.

It mirrors the semantics of the backend's Python client: upsert-style POSTs
resolved by `object_id` or natural `unique_key`, cursor pagination via Go
1.23 range-over-func iterators, and ZITADEL bearer-token authentication.

## Install

```bash
go get gitlab.com/leifwind/stream/terraform-provider-leifwind/client
```

## Usage

```go
package main

import (
	"context"
	"fmt"
	"log"

	"gitlab.com/leifwind/stream/terraform-provider-leifwind/client"
)

func main() {
	c, err := client.New("https://api.leifwind.example",
		client.WithTokenSource(client.ClientCredentials(
			"https://auth.leifwind.example", "client-id", "client-secret",
			client.WithAudience("326102453042806786"))))
	if err != nil {
		log.Fatal(err)
	}
	for p, err := range c.Metadata.IterProjects(context.Background(), client.ListOpts{}) {
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(p.Name, p.ObjectID)
	}
}
```

`client.ClientCredentials` is one of two supported token sources — the other
being a static/delegated bearer token (`client.StaticToken` or handing a
`TokenSource` from your own auth flow). Use whichever matches how your
service acquires credentials; this mirrors the two auth paths the Terraform
provider itself exposes (see the root [`README.md`](../README.md)).

### Known backend quirks to be aware of

- **Project names are globally unique across all tenants**, not just within
  a single organization (LW-71) — a consequence of the schema-per-project
  design (each project maps to its own Postgres schema, and schema names are
  database-global). Namespace names accordingly if you create projects
  programmatically.
- **A FRAGMENT field requires a sibling KEY field** on the entity: creating a
  FRAGMENT field on a keyless entity, or deleting the entity's last KEY field
  while FRAGMENT fields remain, is rejected (422). Create KEY fields before
  FRAGMENT fields, and delete FRAGMENT fields before the last KEY field.

## Testing your own code against a real stack

The `leifwindtest` package (`client/leifwindtest`) is an exported test
fixture, usable by any consumer of this client — not just this repository's
own test suite. It boots ZITADEL, the leifwind backend, and PostgreSQL in
Docker via testcontainers, and mints per-organization tokens for you:

```go
var sharedStack *leifwindtest.Stack

func TestMain(m *testing.M) {
	var cleanup func()
	var err error
	sharedStack, cleanup, err = leifwindtest.StartMain()
	if err != nil {
		log.Fatal(err)
	}
	code := m.Run()
	cleanup() // NOT deferred: os.Exit below skips deferred calls
	os.Exit(code)
}
```

From there, `sharedStack.NewOrg(t)` gives each test its own organization and
machine-user token (`org.TokenSource(sharedStack)` plugs straight into
`client.WithTokenSource`), so tests get tenant isolation via one org per
test and can run with `t.Parallel()` against a single shared stack rather
than paying the ~1–2 minute stack-boot cost per test.

Requires Docker locally (and `docker login registry.gitlab.com` once, with a
`read_registry` personal access token, to pull the private backend test
image).
