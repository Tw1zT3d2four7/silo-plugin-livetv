// Package runtime implements the live TV plugin's Runtime gRPC server.
package runtime

import (
	"context"
	"fmt"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	"github.com/Silo-Server/silo-plugin-sdk/pkg/pluginsdk/runtimedefault"
)

// ConfigureHandler initializes the plugin using the host's saved global configuration.
type ConfigureHandler func(context.Context, []*pluginv1.ConfigEntry) error

// Server implements the plugin's Runtime service.
type Server struct {
	runtimedefault.Server
	manifest *pluginv1.PluginManifest
	configure ConfigureHandler
}

// New constructs a runtime server bound to the given manifest and optional configure handler.
func New(manifest *pluginv1.PluginManifest, handlers ...ConfigureHandler) *Server {
	var configure ConfigureHandler
	if len(handlers) > 0 {
		configure = handlers[0]
	}
	return &Server{manifest: manifest, configure: configure}
}

// GetManifest returns the embedded plugin manifest.
func (s *Server) GetManifest(_ context.Context, _ *pluginv1.GetManifestRequest) (*pluginv1.GetManifestResponse, error) {
	return &pluginv1.GetManifestResponse{Manifest: s.manifest}, nil
}

// Configure applies the host-provided configuration and initializes database-backed services.
func (s *Server) Configure(ctx context.Context, req *pluginv1.ConfigureRequest) (*pluginv1.ConfigureResponse, error) {
	if s.configure == nil {
		return &pluginv1.ConfigureResponse{}, nil
	}
	if req == nil {
		return nil, fmt.Errorf("configure request is required")
	}
	if err := s.configure(ctx, req.GetConfig()); err != nil {
		return nil, err
	}
	return &pluginv1.ConfigureResponse{}, nil
}
