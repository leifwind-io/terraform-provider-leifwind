// SPDX-License-Identifier: MPL-2.0

package leifwindtest

import (
	"fmt"
	"time"

	toxiproxy "github.com/Shopify/toxiproxy/v2/client"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const toxiproxyImage = "ghcr.io/shopify/toxiproxy:2.12.0"

// Toxiproxy returns the control handle for the backend proxy.
// Panics unless the stack was started WithToxiproxy().
func (s *Stack) Toxiproxy() *toxiproxy.Proxy {
	if s.backendProxy == nil {
		panic("leifwindtest: stack started without WithToxiproxy()")
	}
	return s.backendProxy
}

func (s *Stack) startToxiproxy() error {
	ctx := s.ctx
	tp, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:          toxiproxyImage,
			Networks:       []string{s.net.Name},
			NetworkAliases: map[string][]string{s.net.Name: {"toxiproxy"}},
			ExposedPorts:   []string{"8474/tcp", "8666/tcp"},
			WaitingFor:     wait.ForHTTP("/version").WithPort("8474/tcp").WithStartupTimeout(60 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		return fmt.Errorf("toxiproxy: %w", err)
	}
	s.deferCleanup(terminate(ctx, tp))

	host, err := tp.Host(ctx)
	if err != nil {
		return err
	}
	adminPort, err := tp.MappedPort(ctx, "8474/tcp")
	if err != nil {
		return err
	}
	dataPort, err := tp.MappedPort(ctx, "8666/tcp")
	if err != nil {
		return err
	}

	tpc := toxiproxy.NewClient(fmt.Sprintf("%s:%s", host, adminPort.Port()))
	proxy, err := tpc.CreateProxy("backend", "0.0.0.0:8666", "backend:8000")
	if err != nil {
		return fmt.Errorf("create proxy: %w", err)
	}
	s.backendProxy = proxy
	s.ProxiedBackendURL = fmt.Sprintf("http://%s:%s", host, dataPort.Port())
	return nil
}
