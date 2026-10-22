// Command silo-plugin-livetv is the live TV portal plugin entrypoint.
// It serves an IPTV / M3U live TV portal with XMLTV EPG over the Silo
// plugin gRPC surface.
//
// The plugin starts its runtime before opening PostgreSQL so the host can deliver
// saved global configuration through Configure. Database-backed capabilities are wired after configuration.
package main

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"sync"
	goruntime "runtime"
	"strconv"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/jackc/pgx/v5/pgxpool"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	publicmanifest "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginsdk/manifest"
	sdkruntime "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginsdk/runtime"

	"github.com/RXWatcher/silo-plugin-livetv/internal/httpclient"
	"github.com/RXWatcher/silo-plugin-livetv/internal/httproutes"
	"github.com/RXWatcher/silo-plugin-livetv/internal/migrate"
	"github.com/RXWatcher/silo-plugin-livetv/internal/refresh"
	pluginrt "github.com/RXWatcher/silo-plugin-livetv/internal/runtime"
	"github.com/RXWatcher/silo-plugin-livetv/internal/scheduler"
	"github.com/RXWatcher/silo-plugin-livetv/internal/server"
	"github.com/RXWatcher/silo-plugin-livetv/internal/settings"
	"github.com/RXWatcher/silo-plugin-livetv/internal/store"
	"github.com/RXWatcher/silo-plugin-livetv/internal/streamproxy"
)

//go:embed manifest.json
var manifestRaw []byte

func main() {
	logger := hclog.New(&hclog.LoggerOptions{Name: "silo-plugin-livetv"})

	manifest, err := loadManifest()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load manifest: %v\n", err)
		os.Exit(1)
	}

	// Serve the manifest subcommand before requiring runtime config — the
	// host extracts the manifest at install time, before any config exists.
	if len(os.Args) > 1 && os.Args[1] == "manifest" {
		sdkruntime.Serve(sdkruntime.ServeConfig{
			Logger:  logger,
			Servers: sdkruntime.CapabilityServers{Runtime: pluginrt.New(manifest)},
		})
		return
	}


	// Database-dependent services are initialized by the host's Configure RPC.
	// The host starts this process before sending saved global configuration.
	httpSrv := httproutes.NewServer()
	var depsMu sync.RWMutex
	var deps *scheduler.Deps
	var poolsMu sync.Mutex
	var pools []*pgxpool.Pool

	rt := pluginrt.New(manifest, func(configCtx context.Context, entries []*pluginv1.ConfigEntry) error {
		dsn := ""
		for _, entry := range entries {
			if entry == nil || entry.GetKey() != "database_url" || entry.GetValue() == nil {
				continue
			}
			if value, ok := entry.GetValue().AsMap()["value"].(string); ok {
				dsn = strings.TrimSpace(value)
			}
		}
		if dsn == "" {
			return fmt.Errorf("required global config database_url.value is missing")
		}
		if err := migrate.Run(configCtx, dsn); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
		pool, err := pgxpool.New(configCtx, dsn)
		if err != nil {
			return fmt.Errorf("pgxpool: %w", err)
		}
		if err := pool.Ping(configCtx); err != nil {
			pool.Close()
			return fmt.Errorf("connect to plugin database: %w", err)
		}
		st := store.New(pool)
		snap, err := settings.Load(configCtx, st)
		if err != nil {
			pool.Close()
			return fmt.Errorf("load settings snapshot: %w", err)
		}

		streamClient := httpclient.Streaming()
		limits := streamproxy.Limits{
			PlaylistCacheTTL: envDuration("PLUGIN_CONFIG_PLAYLIST_CACHE_TTL", 2*time.Second),
			GlobalStreamCap: envInt("PLUGIN_CONFIG_GLOBAL_STREAM_CAP", 500),
			GlobalUpstreamCap: envInt("PLUGIN_CONFIG_GLOBAL_UPSTREAM_CAP", 500),
			PerUserRatePerSec: envFloat("PLUGIN_CONFIG_PER_USER_RATE_PER_SEC", 10),
			PerUserBurst: envFloat("PLUGIN_CONFIG_PER_USER_BURST", 20),
		}
		streamDeps := &streamproxy.Deps{
			Store: st, Settings: snap, Logger: logger.Named("streamproxy"),
			HTTP: streamClient, HTTPForSource: httpclient.StreamingForSource, Limits: limits,
		}
		m3uWorker := &refresh.M3UWorker{Store: st, ClientForSource: httpclient.ShortLivedForSource, Logger: logger.Named("m3u")}
		xmltvWorker := &refresh.XMLTVWorker{Store: st, ClientForSource: httpclient.ShortLivedForSource, Logger: logger.Named("xmltv")}
		srv := &server.Server{
			Store: st, Stream: streamDeps, Settings: snap, Logger: logger.Named("api"),
			M3UWorker: m3uWorker, XMLTVWorker: xmltvWorker, Snapshot: snap,
			GuideCacheTTL: envDuration("PLUGIN_CONFIG_GUIDE_CACHE_TTL", 5*time.Second),
			AuditLogger: logger.Named("audit"),
		}
		reaper := &scheduler.SnapshotReaper{Store: st, Settings: snap, Logger: logger.Named("reaper")}

		httpSrv.SetHandler(srv.Routes())
		depsMu.Lock()
		deps = &scheduler.Deps{Store: st, M3U: m3uWorker, XMLTV: xmltvWorker, Reaper: reaper}
		depsMu.Unlock()
		poolsMu.Lock()
		pools = append(pools, pool)
		poolsMu.Unlock()
		return nil
	})

	sched := scheduler.New(func() *scheduler.Deps {
		depsMu.RLock()
		defer depsMu.RUnlock()
		return deps
	}, logger)

	defer func() {
		poolsMu.Lock()
		defer poolsMu.Unlock()
		for _, pool := range pools {
			pool.Close()
		}
	}()

	sdkruntime.Serve(sdkruntime.ServeConfig{
		Logger: logger,
		Servers: sdkruntime.CapabilityServers{
			Runtime: rt, HttpRoutes: httpSrv, ScheduledTask: sched,
		},
	})
}

// envDuration reads a Go-duration env var, falling back to def when unset or
// unparseable. A blank value is treated as unset (use the default).
func envDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

// envInt reads an integer env var, falling back to def when unset or
// unparseable.
func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// envFloat reads a float env var, falling back to def when unset or
// unparseable.
func envFloat(key string, def float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}

// loadManifest parses the embedded manifest.json and replaces the
// __CHECKSUM__ placeholder with the sha256 of the running binary. The
// host verifies this checksum on registration.
func loadManifest() (*pluginv1.PluginManifest, error) {
	manifest, err := publicmanifest.Load(manifestRaw)
	if err != nil {
		return nil, fmt.Errorf("load embedded manifest: %w", err)
	}
	executablePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve executable path: %w", err)
	}
	binaryData, err := os.ReadFile(executablePath)
	if err != nil {
		return nil, fmt.Errorf("read executable %q: %w", executablePath, err)
	}
	checksum := sha256.Sum256(binaryData)
	manifest.Checksum = hex.EncodeToString(checksum[:])
	if len(manifest.GetSupportedPlatforms()) == 0 {
		manifest.SupportedPlatforms = []*pluginv1.SupportedPlatform{
			{Os: goruntime.GOOS, Arch: goruntime.GOARCH},
		}
	}
	return manifest, nil
}
