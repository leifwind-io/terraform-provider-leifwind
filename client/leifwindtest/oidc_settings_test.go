// SPDX-License-Identifier: MPL-2.0

package leifwindtest

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// fatalRecorder captures Fatalf and panics to stop execution the way a real
// t.Fatalf would (the guard under test must not fall through to HTTP).
type fatalRecorder struct {
	testing.TB
	msg string
}

func (f *fatalRecorder) Fatalf(format string, args ...any) {
	f.msg = fmt.Sprintf(format, args...)
	panic("fatalRecorder")
}
func (f *fatalRecorder) Helper() {}

func TestSetAccessTokenLifetimeRejectsNonWholeSeconds(t *testing.T) {
	t.Parallel()
	for _, d := range []time.Duration{0, -time.Second, 1500 * time.Millisecond} {
		rec := &fatalRecorder{TB: t}
		func() {
			defer func() { _ = recover() }()
			(&Stack{}).SetAccessTokenLifetime(rec, d)
		}()
		if !strings.Contains(rec.msg, "whole number of seconds") {
			t.Errorf("lifetime %v: guard did not fire (msg=%q)", d, rec.msg)
		}
	}
}
