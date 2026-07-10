// SPDX-License-Identifier: MPL-2.0

package leifwindtest

import (
	"fmt"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func (s *Stack) startBackend(withToxiproxy bool) error {
	ctx := s.ctx

	bdb, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:          postgresImage,
			Networks:       []string{s.net.Name},
			NetworkAliases: map[string][]string{s.net.Name: {"backend-db"}},
			Env: map[string]string{
				"POSTGRES_USER":     "leifwind",
				"POSTGRES_PASSWORD": "leifwind",
				"POSTGRES_DB":       "leifwind",
			},
			WaitingFor: wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		return fmt.Errorf("backend-db: %w", err)
	}
	s.deferCleanup(terminate(ctx, bdb))

	backend, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:          BackendImage,
			Networks:       []string{s.net.Name},
			NetworkAliases: map[string][]string{s.net.Name: {"backend"}},
			ExposedPorts:   []string{"8000/tcp"},
			Env: map[string]string{
				"POSTGRES_URL":           "postgresql://leifwind:leifwind@backend-db:5432/leifwind",
				"SERIALIZER_SECRET_KEY":  "test-secret",
				"SERIALIZER_SALT":        "test-salt",
				"OIDC_ISSUER":            s.Issuer,
				"OIDC_AUDIENCE":          s.Audience,
				"OIDC_INTERNAL_BASE_URL": "http://" + zitadelAlias + ":8080",
			},
			// healthz is open; migrations run on startup, allow time
			WaitingFor: wait.ForHTTP("/healthz").WithPort("8000/tcp").
				WithStartupTimeout(120 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		return fmt.Errorf("backend (image %s — check registry login / LW-68 allowlist): %w", BackendImage, err)
	}
	s.deferCleanup(terminate(ctx, backend))

	host, err := backend.Host(ctx)
	if err != nil {
		return err
	}
	port, err := backend.MappedPort(ctx, "8000/tcp")
	if err != nil {
		return err
	}
	s.BackendURL = fmt.Sprintf("http://%s:%s", host, port.Port())

	_ = withToxiproxy // Task 17
	return nil
}
