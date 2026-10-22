// Package runtime implements the live TV plugin's Runtime gRPC server.
package runtime

import (
	"context"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	"github.com/Silo-Server/silo-plugin-sdk/pkg/pluginsdk/runtimedefault"
)

// ConfigureFunc receives the host-managed global configuration for each process start.
type ConfigureFunc func(context.Context, []*pluginv1.ConfigEntry) error

// Server implements the plugin's Runtime service.
type Server struct {
	runtimedefault.Server
	manifest  *pluginv1.PluginManifest
	configure ConfigureFunc
}

// New constructs a runtime server bound to the given manifest and optional configure callback.
func New(manifest *pluginv1.PluginManifest, configure ...ConfigureFunc) *Server {
	s := &Server{manifest: manifest}
	if len(configure) > 0 {
		s.configure = configure[0]
	}
	return s
}

// GetManifest returns the embedded plugin manifest.
func (s *Server) GetManifest(_ context.Context, _ *pluginv1.GetManifestRequest) (*pluginv1.GetManifestResponse, error) {
	return &pluginv1.GetManifestResponse{Manifest: s.manifest}, nil
}

// Configure receives the saved settings from Silo before capability calls are routed.
func (s *Server) Configure(ctx context.Context, req *pluginv1.ConfigureRequest) (*pluginv1.ConfigureResponse, error) {
	if s.configure != nil {
		if err := s.configure(ctx, req.GetConfig()); err != nil {
			return nil, err
		}
	}
	return &pluginv1.ConfigureResponse{}, nil
}
