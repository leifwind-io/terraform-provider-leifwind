// SPDX-License-Identifier: MPL-2.0

package leifwindtest

import (
	"os"
	"testing"
)

// One shared stack for ALL tests in this package (perf: each Start(t) boot
// costs ~60-90s of containers; tests already isolate via per-test orgs, so
// they share one stack the same way the client and acctest packages do).
var (
	mainStack    *Stack
	mainStackErr error
)

func TestMain(m *testing.M) {
	var cleanup func()
	mainStack, cleanup, mainStackErr = StartMain()
	code := m.Run()
	if cleanup != nil {
		cleanup()
	}
	os.Exit(code)
}

// sharedStack returns the package-shared stack, failing the test if the
// TestMain boot failed.
func sharedStack(t testing.TB) *Stack {
	t.Helper()
	if mainStackErr != nil {
		t.Fatalf("shared stack boot failed: %v", mainStackErr)
	}
	return mainStack
}
