// SPDX-License-Identifier: MPL-2.0

package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"runtime/debug"
	"testing"
)

func TestVersionFromBuildInfo(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		bi   *debug.BuildInfo
		want string
	}{
		{"tagged dependency",
			&debug.BuildInfo{Deps: []*debug.Module{{Path: modulePath, Version: "v0.1.0"}}},
			"v0.1.0"},
		{"main module (module's own tests)",
			&debug.BuildInfo{Main: debug.Module{Path: modulePath, Version: "(devel)"}},
			"dev"},
		{"replaced dep (in-repo go.work build)",
			&debug.BuildInfo{Deps: []*debug.Module{{
				Path: modulePath, Version: "v0.0.0-00010101000000-000000000000",
				Replace: &debug.Module{Path: modulePath, Version: "(devel)"},
			}}},
			"dev"},
		{"module absent from build info", &debug.BuildInfo{}, "dev"},
	}
	for _, c := range cases {
		if got := versionFromBuildInfo(c.bi); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestVersionStampsUserAgent(t *testing.T) {
	t.Parallel()
	if Version() == "" {
		t.Fatal("Version() must be non-empty")
	}
	var ua string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte(`{"objects":[],"cursor":null}`))
	}))
	t.Cleanup(srv.Close)
	c, err := New(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Metadata.ListProjects(context.Background(), ListOpts{}); err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^terraform-provider-leifwind-client/\S+$`).MatchString(ua) {
		t.Fatalf("User-Agent = %q, want ^terraform-provider-leifwind-client/<version>", ua)
	}
}
