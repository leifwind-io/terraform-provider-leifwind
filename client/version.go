// SPDX-License-Identifier: MPL-2.0

package client

import (
	"runtime/debug"
	"sync"
)

// modulePath must match this module's declared path — it is how Version
// finds our own entry in the consumer's build info.
const modulePath = "gitlab.com/leifwind/stream/terraform-provider-leifwind/client"

var cachedVersion = sync.OnceValue(func() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	return versionFromBuildInfo(bi)
})

// Version reports this module's version as recorded in the running binary's
// build info: the module tag (e.g. "v0.1.0") when consumed as a dependency,
// "dev" for in-repo go.work builds and other unstamped binaries.
func Version() string { return cachedVersion() }

func versionFromBuildInfo(bi *debug.BuildInfo) string {
	v := ""
	if bi.Main.Path == modulePath {
		v = bi.Main.Version
	}
	for _, dep := range bi.Deps {
		if dep.Path != modulePath {
			continue
		}
		v = dep.Version
		if dep.Replace != nil {
			v = dep.Replace.Version // local replace / go.work: "(devel)" or ""
		}
	}
	if v == "" || v == "(devel)" {
		return "dev"
	}
	return v
}
