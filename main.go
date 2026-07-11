// SPDX-License-Identifier: MPL-2.0

// Package main is the terraform-provider-leifwind binary entry point.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"gitlab.com/leifwind/stream/terraform-provider-leifwind/internal/provider"
)

// version is set by goreleaser via ldflags.
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "run with support for debuggers like delve")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/leifwind-io/leifwind",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err)
	}
}
