// SPDX-License-Identifier: MPL-2.0

package acctest

import (
	"fmt"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("TF_ACC") == "" {
		// acceptance gate: plain `go test` skips container boot entirely
		os.Exit(m.Run())
	}
	if err := startShared(); err != nil {
		fmt.Fprintf(os.Stderr, "leifwindtest stack: %v\n", err)
		os.Exit(1)
	}
	code := m.Run()
	stopShared()
	os.Exit(code)
}

func TestProviderFactorySmoke(t *testing.T) {
	// compile-time smoke: the factory must produce a protocol-6 server
	if _, err := ProtoV6ProviderFactories()["leifwind"](); err != nil {
		t.Fatal(err)
	}
}
