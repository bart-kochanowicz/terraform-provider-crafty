package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/bart-kochanowicz/terraform-provider-crafty/internal/provider"
)

var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "Print provider version and exit")
	debug := flag.Bool("debug", false, "Run with debugger support")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{Address: "registry.terraform.io/bart-kochanowicz/crafty", Debug: *debug})
	if err != nil {
		log.Fatal(err)
	}
}
