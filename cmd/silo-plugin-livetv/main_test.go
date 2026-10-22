package main

import (
	"testing"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestConfiguredDatabaseURL(t *testing.T) {
	value, err := structpb.NewStruct(map[string]any{
		"value": "  postgres://plugin_livetv:silo@postgres:5432/silo?search_path=livetv&sslmode=disable  ",
	})
	if err != nil {
		t.Fatal(err)
	}

	got := configuredDatabaseURL([]*pluginv1.ConfigEntry{
		{Key: "other", Value: value},
		{Key: "database_url", Value: value},
	})
	want := "postgres://plugin_livetv:silo@postgres:5432/silo?search_path=livetv&sslmode=disable"
	if got != want {
		t.Fatalf("configuredDatabaseURL() = %q, want %q", got, want)
	}
}

func TestConfiguredDatabaseURLMissing(t *testing.T) {
	if got := configuredDatabaseURL(nil); got != "" {
		t.Fatalf("configuredDatabaseURL(nil) = %q, want empty", got)
	}

	value, err := structpb.NewStruct(map[string]any{"other": "value"})
	if err != nil {
		t.Fatal(err)
	}
	if got := configuredDatabaseURL([]*pluginv1.ConfigEntry{{Key: "database_url", Value: value}}); got != "" {
		t.Fatalf("configuredDatabaseURL(without value field) = %q, want empty", got)
	}
}
