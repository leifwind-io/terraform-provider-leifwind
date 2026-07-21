// SPDX-License-Identifier: MPL-2.0

package leifwindtest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// httpClient bounds every fixture HTTP call; the per-site deadline loops
// only check between requests, so a hung connection must fail on its own.
var httpClient = &http.Client{Timeout: 15 * time.Second}

func freePort() (int, error) {
	l, err := net.Listen("tcp", ":0") //nolint:gosec // test-only: binds to all interfaces to probe a free port, not to serve
	if err != nil {
		return 0, err
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func (s *Stack) startZitadel() error {
	ctx := s.ctx

	// zitadel-db
	zdb, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:          postgresImage,
			Networks:       []string{s.net.Name},
			NetworkAliases: map[string][]string{s.net.Name: {"zitadel-db"}},
			Env: map[string]string{
				"POSTGRES_USER":     "zitadel",
				"POSTGRES_PASSWORD": "zitadel",
				"POSTGRES_DB":       "zitadel",
			},
			WaitingFor: wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		return fmt.Errorf("zitadel-db: %w", err)
	}
	s.deferCleanup(terminate(ctx, zdb))

	// EXTERNALDOMAIN/PORT chicken-and-egg: pick the host port up front.
	provider, err := testcontainers.NewDockerProvider()
	if err != nil {
		return err
	}
	defer func() { _ = provider.Close() }()
	host, err := provider.DaemonHost(ctx)
	if err != nil {
		return err
	}
	port, err := freePort()
	if err != nil {
		return err
	}
	s.Issuer = fmt.Sprintf("http://%s:%d", host, port)

	volumeName := "zitadel-machinekey-" + uuid.NewString()[:12]

	zc, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:          zitadelImage,
			Networks:       []string{s.net.Name},
			NetworkAliases: map[string][]string{s.net.Name: {zitadelAlias}},
			Cmd:            []string{"start-from-init", "--masterkeyFromEnv", "--tlsMode", "disabled"},
			Env: map[string]string{
				"ZITADEL_MASTERKEY":                                    zitadelMasterkey,
				"ZITADEL_DATABASE_POSTGRES_HOST":                       "zitadel-db",
				"ZITADEL_DATABASE_POSTGRES_PORT":                       "5432",
				"ZITADEL_DATABASE_POSTGRES_DATABASE":                   "zitadel",
				"ZITADEL_DATABASE_POSTGRES_USER_USERNAME":              "zitadel",
				"ZITADEL_DATABASE_POSTGRES_USER_PASSWORD":              "zitadel",
				"ZITADEL_DATABASE_POSTGRES_USER_SSL_MODE":              "disable",
				"ZITADEL_DATABASE_POSTGRES_ADMIN_USERNAME":             "zitadel",
				"ZITADEL_DATABASE_POSTGRES_ADMIN_PASSWORD":             "zitadel",
				"ZITADEL_DATABASE_POSTGRES_ADMIN_SSL_MODE":             "disable",
				"ZITADEL_EXTERNALDOMAIN":                               host,
				"ZITADEL_EXTERNALPORT":                                 strconv.Itoa(port),
				"ZITADEL_EXTERNALSECURE":                               "false",
				"ZITADEL_TLS_ENABLED":                                  "false",
				"ZITADEL_FIRSTINSTANCE_ORG_NAME":                       "leifwind-test",
				"ZITADEL_FIRSTINSTANCE_ORG_HUMAN_USERNAME":             "admin",
				"ZITADEL_FIRSTINSTANCE_ORG_HUMAN_PASSWORD":             "Password1!",
				"ZITADEL_FIRSTINSTANCE_ORG_MACHINE_MACHINE_USERNAME":   "bootstrap",
				"ZITADEL_FIRSTINSTANCE_ORG_MACHINE_MACHINE_NAME":       "bootstrap",
				"ZITADEL_FIRSTINSTANCE_PATPATH":                        patPath,
				"ZITADEL_FIRSTINSTANCE_ORG_MACHINE_PAT_EXPIRATIONDATE": "2035-01-01T00:00:00Z",
			},
			HostConfigModifier: func(hc *container.HostConfig) {
				// container runs as root to create /machinekey (dev/test only);
				// named docker-managed volume — a host bind would resolve on the
				// daemon's filesystem under dind, silently breaking PAT readback.
				hc.Mounts = append(hc.Mounts, mount.Mount{
					Type: mount.TypeVolume, Source: volumeName, Target: "/machinekey",
				})
				hc.PortBindings = network.PortMap{
					network.MustParsePort("8080/tcp"): []network.PortBinding{
						{HostIP: netip.IPv4Unspecified(), HostPort: strconv.Itoa(port)},
					},
				}
			},
			ConfigModifier: func(c *container.Config) { c.User = "0" },
			ExposedPorts:   []string{"8080/tcp"},
		},
		Started: true,
	})
	if err != nil {
		return fmt.Errorf("zitadel: %w", err)
	}
	s.zitadel = zc
	// Registered before terminate(zc): cleanup runs LIFO, so the container
	// is stopped and removed first and the volume is no longer in use by
	// the time removal is attempted.
	s.deferCleanup(func() {
		if cli, err := testcontainers.NewDockerClientWithOpts(ctx); err == nil {
			_, _ = cli.VolumeRemove(ctx, volumeName, client.VolumeRemoveOptions{Force: true})
			_ = cli.Close()
		}
	})
	s.deferCleanup(terminate(ctx, zc))

	if err := s.waitZitadelReady(); err != nil {
		return err
	}
	pat, err := s.waitForPAT()
	if err != nil {
		return err
	}
	s.mgmtPAT = pat

	// default org + API project (its id is the OIDC audience)
	var org struct {
		Org struct {
			ID string `json:"id"`
		} `json:"org"`
	}
	if err := s.mgmtDo("GET", "/management/v1/orgs/me", "", nil, &org); err != nil {
		return fmt.Errorf("orgs/me: %w", err)
	}
	s.defaultOrgID = org.Org.ID

	var proj struct {
		ID string `json:"id"`
	}
	if err := s.mgmtDo("POST", "/management/v1/projects",
		"", map[string]string{"name": "leifwind-api"}, &proj); err != nil {
		return fmt.Errorf("create project: %w", err)
	}
	s.Audience = proj.ID
	return nil
}

// waitZitadelReady: native probe first (no HTTP/Host header), then discovery
// from the host (exercises instance resolution / EXTERNALDOMAIN config).
func (s *Stack) waitZitadelReady() error {
	deadline := time.Now().Add(120 * time.Second)
	for {
		code, _, err := s.zitadel.Exec(s.ctx, []string{"/app/zitadel", "ready"})
		if err == nil && code == 0 {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("zitadel ready probe timed out")
		}
		time.Sleep(2 * time.Second)
	}
	for {
		resp, err := httpClient.Get(s.Issuer + "/.well-known/openid-configuration")
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == 200 {
				return nil
			}
			if resp.StatusCode == 404 && bytes.Contains(body, []byte("QUERY-")) {
				return fmt.Errorf("instance not found — domain misconfig: %s", body)
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("zitadel discovery timed out")
		}
		time.Sleep(2 * time.Second)
	}
}

// waitForPAT reads the bootstrap PAT via the docker archive API
// (distroless image: no shell; daemon may be remote under dind).
func (s *Stack) waitForPAT() (string, error) {
	deadline := time.Now().Add(30 * time.Second)
	for {
		rc, err := s.zitadel.CopyFileFromContainer(s.ctx, patPath)
		if err == nil {
			b, rerr := io.ReadAll(rc)
			_ = rc.Close()
			if rerr == nil && len(bytes.TrimSpace(b)) > 0 {
				return string(bytes.TrimSpace(b)), nil
			}
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("PAT never appeared at %s", patPath)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// mgmtDo calls a ZITADEL management/v2 API with the bootstrap PAT,
// retrying 503 for ~30s (the query side settles after 'ready').
func (s *Stack) mgmtDo(method, path, orgID string, body, out any) error {
	deadline := time.Now().Add(30 * time.Second)
	for {
		var rdr io.Reader
		if body != nil {
			b, err := json.Marshal(body)
			if err != nil {
				return err
			}
			rdr = bytes.NewReader(b)
		}
		req, err := http.NewRequestWithContext(s.ctx, method, s.Issuer+path, rdr)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+s.mgmtPAT)
		req.Header.Set("Content-Type", "application/json")
		if orgID != "" {
			req.Header.Set("x-zitadel-orgid", orgID)
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			return err
		}
		rb, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode == 503 && time.Now().Before(deadline) {
			select {
			case <-s.ctx.Done():
				return s.ctx.Err()
			case <-time.After(time.Second):
			}
			continue
		}
		if resp.StatusCode >= 400 {
			return fmt.Errorf("%s %s: %d %s", method, path, resp.StatusCode, rb)
		}
		if out != nil {
			return json.Unmarshal(rb, out)
		}
		return nil
	}
}
