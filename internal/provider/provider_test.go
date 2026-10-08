package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/bart-kochanowicz/terraform-provider-crafty/internal/client"
)

func TestProviderMetadataAndResourceRegistration(t *testing.T) {
	ctx := context.Background()
	p := New("1.2.3")()
	var metadata provider.MetadataResponse
	p.Metadata(ctx, provider.MetadataRequest{}, &metadata)
	if metadata.TypeName != "crafty" || metadata.Version != "1.2.3" {
		t.Fatalf("unexpected metadata: %+v", metadata)
	}
	resources := p.Resources(ctx)
	if len(resources) != 3 {
		t.Fatalf("expected three resources, got %d", len(resources))
	}
	var resourceMetadata resource.MetadataResponse
	resources[0]().Metadata(ctx, resource.MetadataRequest{ProviderTypeName: metadata.TypeName}, &resourceMetadata)
	if resourceMetadata.TypeName != "crafty_minecraft_server" {
		t.Fatalf("unexpected resource type: %s", resourceMetadata.TypeName)
	}
	sources := p.DataSources(ctx)
	if len(sources) != 1 {
		t.Fatalf("expected one data source, got %d", len(sources))
	}
	var sourceMetadata datasource.MetadataResponse
	sources[0]().Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: metadata.TypeName}, &sourceMetadata)
	if sourceMetadata.TypeName != "crafty_server" {
		t.Fatalf("unexpected data source type: %s", sourceMetadata.TypeName)
	}
}

func TestProviderConfigure(t *testing.T) {
	for _, test := range []struct {
		name       string
		url, token types.String
		wantError  bool
	}{
		{"valid", types.StringValue("https://crafty.example.com:8443/"), types.StringValue("secret"), false},
		{"invalid URL", types.StringValue("crafty.example.com"), types.StringValue("secret"), true},
		{"URL credentials", types.StringValue("https://user:password@crafty.example.com"), types.StringValue("secret"), true},
		{"empty token", types.StringValue("https://crafty.example.com"), types.StringValue(""), true},
		{"unknown token", types.StringValue("https://crafty.example.com"), types.StringUnknown(), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			p := New("test")()
			var schema provider.SchemaResponse
			p.Schema(ctx, provider.SchemaRequest{}, &schema)
			plan := tfsdk.Plan{Schema: schema.Schema}
			if d := plan.Set(ctx, &providerModel{URL: test.url, Token: test.token}); d.HasError() {
				t.Fatal(d)
			}
			var configured provider.ConfigureResponse
			p.Configure(ctx, provider.ConfigureRequest{Config: tfsdk.Config{Schema: schema.Schema, Raw: plan.Raw}}, &configured)
			if configured.Diagnostics.HasError() != test.wantError {
				t.Fatalf("unexpected diagnostics: %v", configured.Diagnostics)
			}
			if !test.wantError {
				if _, ok := configured.ResourceData.(*client.Client); !ok {
					t.Fatalf("unexpected provider client: %T", configured.ResourceData)
				}
				if configured.DataSourceData != configured.ResourceData {
					t.Fatal("resources and data sources must share the configured client")
				}
			}
		})
	}
}
