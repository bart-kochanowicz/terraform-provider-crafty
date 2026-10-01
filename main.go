package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/bart-kochanowicz/terraform-provider-crafty/internal/provider"
)

var version = "dev"

func main() {
	debug := flag.Bool("debug", false, "Run with debugger support")
	flag.Parse()
	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{Address: "registry.terraform.io/bart-kochanowicz/crafty", Debug: *debug})
	if err != nil {
		log.Fatal(err)
	}
}
