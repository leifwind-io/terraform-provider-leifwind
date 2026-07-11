// SPDX-License-Identifier: MPL-2.0

// Package leifwindtest boots a real leifwind stack (ZITADEL v4.15.3,
// backend, PostgreSQL) in testcontainers for blackbox tests. It is a
// public package: client consumers may use it for their own tests.
package leifwindtest

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
)

// BackendImage is the backend under test.
// TODO(LW-68): pin semver once the backend cuts a release.
const BackendImage = "registry.gitlab.com/leifwind/stream/backend:edge"

const (
	zitadelImage  = "ghcr.io/zitadel/zitadel:v4.15.3"
	postgresImage = "postgres:18-alpine"
	zitadelAlias  = "zitadel"
	// dev/test-only masterkey, mirrors backend testing.py
	zitadelMasterkey = "MasterkeyNeedsToHave32Characters"
	patPath          = "/machinekey/bootstrap-pat.txt"
)

type stackSettings struct {
	toxiproxy bool
}

// StackOption configures Start/StartMain.
type StackOption func(*stackSettings)

// WithToxiproxy routes ProxiedBackendURL through a toxiproxy container
// (fault injection; fully wired in the retry task).
func WithToxiproxy() StackOption {
	return func(s *stackSettings) { s.toxiproxy = true }
}

// Stack is a running leifwind stack.
type Stack struct {
	Issuer            string // ZITADEL external URL (token iss)
	Audience          string // ZITADEL API project id (token aud)
	BackendURL        string // set by startBackend (Task 11)
	ProxiedBackendURL string // set by WithToxiproxy (Task 17)

	ctx          context.Context
	mgmtPAT      string
	defaultOrgID string
	net          *testcontainers.DockerNetwork
	zitadel      testcontainers.Container
	teardown     []func()

	// exchangeSetup and exchangeApp* back UserToken's one-time-per-Stack RFC
	// 8693 setup (feature flag + impersonation policy + token-exchange OIDC
	// app). Per-Stack, not package-level: each Stack is its own ZITADEL
	// instance/project, so a process-wide sync.Once would silently reuse a
	// previous (possibly torn-down) Stack's app credentials for any second
	// Stack in the same test binary.
	exchangeSetup           sync.Once
	exchangeAppClientID     string
	exchangeAppClientSecret string
}

// Start boots the stack and registers cleanup on t.
func Start(t testing.TB, opts ...StackOption) *Stack {
	t.Helper()
	s, cleanup, err := StartMain(opts...)
	if err != nil {
		t.Fatalf("leifwindtest: %v", err)
	}
	t.Cleanup(cleanup)
	return s
}

// StartMain is the TestMain-friendly variant (no testing.TB required).
func StartMain(opts ...StackOption) (*Stack, func(), error) {
	var settings stackSettings
	for _, o := range opts {
		o(&settings)
	}
	ctx := context.Background()
	s := &Stack{ctx: ctx}

	net, err := network.New(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("network: %w", err)
	}
	s.net = net
	s.deferCleanup(func() { _ = net.Remove(ctx) })

	if err := s.startZitadel(); err != nil {
		s.cleanup()
		return nil, nil, err
	}
	if err := s.startBackend(settings.toxiproxy); err != nil {
		s.cleanup()
		return nil, nil, err
	}
	return s, s.cleanup, nil
}

func (s *Stack) deferCleanup(f func()) { s.teardown = append(s.teardown, f) }

func (s *Stack) cleanup() {
	for i := len(s.teardown) - 1; i >= 0; i-- {
		s.teardown[i]()
	}
	s.teardown = nil
}

func terminate(ctx context.Context, c testcontainers.Container) func() {
	return func() {
		tctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		_ = c.Terminate(tctx)
	}
}
