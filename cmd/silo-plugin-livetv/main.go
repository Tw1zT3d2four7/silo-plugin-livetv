// Command silo-plugin-livetv is the Live TV portal plugin entrypoint.
package main

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"os"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/jackc/pgx/v5/pgxpool"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	publicmanifest "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginsdk/manifest"
	sdkruntime "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginsdk/runtime"

	"github.com/RXWatcher/silo-plugin-livetv/internal/httpclient"
	"github.com/RXWatcher/silo-plugin-livetv/internal/httproutes"
	"github.com/RXWatcher/silo-plugin-livetv/internal/migrate"
	pluginrt "github.com/RXWatcher/silo-plugin-livetv/internal/runtime"
	"github.com/RXWatcher/silo-plugin-livetv/internal/refresh"
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

	// The host runs this subcommand while installing the plugin, before any
	// operator configuration exists.
	if len(os.Args) > 1 && os.Args[1] == "manifest" {
		sdkruntime.Serve(sdkruntime.ServeConfig{
			Logger:  logger,
			Servers: sdkruntime.CapabilityServers{Runtime: pluginrt.New(manifest)},
		})
		return
	}

	httpSrv := httproutes.NewServer()
	var depsMu sync.RWMutex
	var scheduledDeps *scheduler.Deps
	var pool *pgxpool.Pool

	sched := scheduler.New(func() *scheduler.Deps {
		depsMu.RLock()
		defer depsMu.RUnlock()
		if scheduledDeps == nil {
			return nil
		}
		copy := *scheduledDeps
		return &copy
	}, logger)

	// Silo supplies global_config_schema values through Runtime.Configure,
	// not process environment variables. Initialise the DB-backed capabilities
	// only after receiving that configuration.
	configure := func(ctx context.Context, entries []*pluginv1.ConfigEntry) error {
		dsn := configuredDatabaseURL(entries)
		if dsn == "" {
			logger.Warn("Live TV is not configured yet: save the Postgres connection string in the plugin settings")
			return nil
		}

		if err := migrate.Run(ctx, dsn); err != nil {
			return fmt.Errorf("migrate Live TV database: %w", err)
		}
		newPool, err := pgxpool.New(ctx, dsn)
		if err != nil {
			return fmt.Errorf("open Live TV database pool: %w", err)
		}

		st := store.New(newPool)
		snap, err := settings.Load(ctx, st)
		if err != nil {
			newPool.Close()
			return fmt.Errorf("load Live TV settings: %w", err)
		}

		streamClient := httpclient.Streaming()
		refreshClient := httpclient.ShortLived()
		limits := streamproxy.Limits{
			PlaylistCacheTTL:  envDuration("PLUGIN_CONFIG_PLAYLIST_CACHE_TTL", 2*time.Second),
			GlobalStreamCap:   envInt("PLUGIN_CONFIG_GLOBAL_STREAM_CAP", 500),
			GlobalUpstreamCap: envInt("PLUGIN_CONFIG_GLOBAL_UPSTREAM_CAP", 500),
			PerUserRatePerSec: envFloat("PLUGIN_CONFIG_PER_USER_RATE_PER_SEC", 10),
			PerUserBurst:      envFloat("PLUGIN_CONFIG_PER_USER_BURST", 20),
		}
		streamDeps := &streamproxy.Deps{
			Store: st, Settings: snap, Logger: logger.Named("streamproxy"),
			HTTP: streamClient, Limits: limits,
		}
		m3uWorker := &refresh.M3UWorker{Store: st, Client: refreshClient, Logger: logger.Named("m3u")}
		xmltvWorker := &refresh.XMLTVWorker{Store: st, Client: refreshClient, Logger: logger.Named("xmltv")}
		srv := &server.Server{
			Store: st, Stream: streamDeps, Settings: snap, Logger: logger.Named("api"),
			M3UWorker: m3uWorker, XMLTVWorker: xmltvWorker, Snapshot: snap,
			GuideCacheTTL: envDuration("PLUGIN_CONFIG_GUIDE_CACHE_TTL", 5*time.Second),
			AuditLogger: logger.Named("audit"),
		}
		reaper := &scheduler.SnapshotReaper{Store: st, Settings: snap, Logger: logger.Named("reaper")}

		// Publish the fully constructed HTTP and scheduled-task dependencies
		// together, after all DB setup has succeeded.
		httpSrv.SetHandler(srv.Routes())
		depsMu.Lock()
		scheduledDeps = &scheduler.Deps{
			Store: st, M3U: m3uWorker, XMLTV: xmltvWorker, Reaper: reaper,
		}
		pool = newPool
		depsMu.Unlock()
		logger.Info("Live TV plugin configured")
		return nil
	}

	rt := pluginrt.New(manifest, configure)
	sdkruntime.Serve(sdkruntime.ServeConfig{
		Logger: logger,
		Servers: sdkruntime.CapabilityServers{
			Runtime: rt, HttpRoutes: httpSrv, ScheduledTask: sched,
		},
	})
	if pool != nil {
		pool.Close()
	}
}

// configuredDatabaseURL extracts the connection string from Silo's saved
// global config entry: database_url = {"value": "<dsn>"}.
func configuredDatabaseURL(entries []*pluginv1.ConfigEntry) string {
	for _, entry := range entries {
		if entry == nil || entry.GetKey() != "database_url" || entry.GetValue() == nil {
			continue
		}
		value, ok := entry.GetValue().AsMap()["value"].(string)
		if ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
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
		manifest.SupportedPlatforms = []*pluginv1.SupportedPlatform{{Os: goruntime.GOOS, Arch: goruntime.GOARCH}}
	}
	return manifest, nil
}
