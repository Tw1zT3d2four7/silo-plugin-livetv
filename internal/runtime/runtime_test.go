package runtime

import (
	"context"
	"errors"
	"testing"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestConfigurePassesHostConfigToHandler(t *testing.T) {
	value, err := structpb.NewStruct(map[string]any{"value": "postgres://example/test"})
	if err != nil {
		t.Fatal(err)
	}
	want := []*pluginv1.ConfigEntry{{Key: "database_url", Value: value}}
	called := false
	s := New(&pluginv1.PluginManifest{}, func(_ context.Context, got []*pluginv1.ConfigEntry) error {
		called = true
		if len(got) != 1 || got[0].GetKey() != want[0].GetKey() {
			t.Fatalf("unexpected config entries: %#v", got)
		}
		if got[0].GetValue().AsMap()["value"] != "postgres://example/test" {
			t.Fatalf("unexpected database_url value: %#v", got[0].GetValue().AsMap())
		}
		return nil
	})

	if _, err := s.Configure(context.Background(), &pluginv1.ConfigureRequest{Config: want}); err != nil {
		t.Fatalf("Configure returned error: %v", err)
	}
	if !called {
		t.Fatal("Configure handler was not called")
	}
}

func TestConfigureReturnsInitializationError(t *testing.T) {
	wantErr := errors.New("database initialization failed")
	s := New(&pluginv1.PluginManifest{}, func(context.Context, []*pluginv1.ConfigEntry) error {
		return wantErr
	})

	if _, err := s.Configure(context.Background(), &pluginv1.ConfigureRequest{}); !errors.Is(err, wantErr) {
		t.Fatalf("Configure error = %v, want %v", err, wantErr)
	}
}

func TestConfigureWithoutHandlerIsNoop(t *testing.T) {
	s := New(&pluginv1.PluginManifest{})
	if _, err := s.Configure(context.Background(), &pluginv1.ConfigureRequest{}); err != nil {
		t.Fatalf("Configure without handler returned error: %v", err)
	}
}
