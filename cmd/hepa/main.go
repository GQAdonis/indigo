package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	_ "net/http/pprof"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/identity/redisdir"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/indigo/automod/capture"

	"github.com/carlmjohnson/versioninfo"
	_ "github.com/joho/godotenv/autoload"
	cli "github.com/urfave/cli/v2"
	"golang.org/x/time/rate"
)

func main() {
	if err := run(os.Args); err != nil {
		slog.Error("exiting", "err", err)
		os.Exit(-1)
	}
}

func run(args []string) error {

	app := cli.App{
		Name:    "hepa",
		Usage:   "automod daemon (cleans the atmosphere)",
		Version: versioninfo.Short(),
	}

	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:    "atp-bgs-host",
			Usage:   "hostname and port of BGS to subscribe to",
			Value:   "wss://bsky.network",
			EnvVars: []string{"ATP_BGS_HOST"},
		},
		&cli.StringFlag{
			Name:    "atp-plc-host",
			Usage:   "method, hostname, and port of PLC registry",
			Value:   "https://plc.directory",
			EnvVars: []string{"ATP_PLC_HOST"},
		},
		&cli.StringFlag{
			Name:    "atp-mod-host",
			Usage:   "method, hostname, and port of moderation service",
			Value:   "https://api.bsky.app",
			EnvVars: []string{"ATP_MOD_HOST"},
		},
		&cli.StringFlag{
			Name:    "atp-bsky-host",
			Usage:   "method, hostname, and port of bsky API (appview) service",
			Value:   "https://api.bsky.app",
			EnvVars: []string{"ATP_BSKY_HOST"},
		},
		&cli.StringFlag{
			Name:  "redis-url",
			Usage: "redis connection URL",
			// redis://<user>:<pass>@localhost:6379/<db>
			// redis://localhost:6379/0
			EnvVars: []string{"HEPA_REDIS_URL"},
		},
		&cli.StringFlag{
			Name:    "mod-handle",
			Usage:   "for mod service login",
			EnvVars: []string{"HEPA_MOD_AUTH_HANDLE"},
		},
		&cli.StringFlag{
			Name:    "mod-password",
			Usage:   "for mod service login",
			EnvVars: []string{"HEPA_MOD_AUTH_PASSWORD"},
		},
		&cli.StringFlag{
			Name:    "mod-admin-token",
			Usage:   "admin authentication password for mod service",
			EnvVars: []string{"HEPA_MOD_AUTH_ADMIN_TOKEN"},
		},
		&cli.IntFlag{
			Name:    "plc-rate-limit",
			Usage:   "max number of requests per second to PLC registry",
			Value:   100,
			EnvVars: []string{"HEPA_PLC_RATE_LIMIT"},
		},
		&cli.StringFlag{
			Name:    "sets-json-path",
			Usage:   "file path of JSON file containing static sets",
			EnvVars: []string{"HEPA_SETS_JSON_PATH"},
		},
		&cli.StringFlag{
			Name:    "hiveai-api-token",
			Usage:   "API token for Hive AI image auto-labeling",
			EnvVars: []string{"HIVEAI_API_TOKEN"},
		},
		&cli.StringFlag{
			Name:    "abyss-host",
			Usage:   "host for abusive image scanning API (scheme, host, port)",
			EnvVars: []string{"ABYSS_HOST"},
		},
		&cli.StringFlag{
			Name:    "abyss-password",
			Usage:   "admin auth password for abyss API",
			EnvVars: []string{"ABYSS_PASSWORD"},
		},
		&cli.StringFlag{
			Name:    "ruleset",
			Usage:   "which ruleset config to use: default, no-blobs, only-blobs",
			EnvVars: []string{"HEPA_RULESET"},
		},
		&cli.StringFlag{
			Name:    "log-level",
			Usage:   "log verbosity level (eg: warn, info, debug)",
			EnvVars: []string{"HEPA_LOG_LEVEL", "LOG_LEVEL"},
		},
		&cli.StringFlag{
			Name:    "ratelimit-bypass",
			Usage:   "HTTP header to bypass ratelimits",
			EnvVars: []string{"HEPA_RATELIMIT_BYPASS", "RATELIMIT_BYPASS"},
		},
	}

	app.Commands = []*cli.Command{
		runCmd,
		processRecordCmd,
		processRecentCmd,
		captureRecentCmd,
	}

	return app.Run(args)
}

func configDirectory(cctx *cli.Context) (identity.Directory, error) {
	baseDir := identity.BaseDirectory{
		PLCURL: cctx.String("atp-plc-host"),
		HTTPClient: http.Client{
			Timeout: time.Second * 15,
		},
		PLCLimiter:            rate.NewLimiter(rate.Limit(cctx.Int("plc-rate-limit")), 1),
		TryAuthoritativeDNS:   true,
		SkipDNSDomainSuffixes: []string{".bsky.social", ".staging.bsky.dev"},
	}
	var dir identity.Directory
	if cctx.String("redis-url") != "" {
		rdir, err := redisdir.NewRedisDirectory(&baseDir, cctx.String("redis-url"), time.Hour*24, time.Minute*2, 10_000)
		if err != nil {
			return nil, err
		}
		dir = rdir
	} else {
		cdir := identity.NewCacheDirectory(&baseDir, 1_500_000, time.Hour*24, time.Minute*2)
		dir = &cdir
	}
	return dir, nil
}

func configLogger(cctx *cli.Context, writer io.Writer) *slog.Logger {
	var level slog.Level
	switch strings.ToLower(cctx.String("log-level")) {
	case "error":
		level = slog.LevelError
	case "warn":
		level = slog.LevelWarn
	case "info":
		level = slog.LevelInfo
	case "debug":
		level = slog.LevelDebug
	default:
		level = slog.LevelInfo
	}
	logger := slog.New(slog.NewJSONHandler(writer, &slog.HandlerOptions{
		Level: level,
	}))
	slog.SetDefault(logger)
	return logger
}

var runCmd = &cli.Command{
	Name:  "run",
	Usage: "run the hepa daemon",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "metrics-listen",
			Usage:   "IP or address, and port, to listen on for metrics APIs",
			Value:   ":3989",
			EnvVars: []string{"HEPA_METRICS_LISTEN"},
		},
		&cli.StringFlag{
			Name: "slack-webhook-url",
			// eg: https://hooks.slack.com/services/X1234
			Usage:   "full URL of slack webhook",
			EnvVars: []string{"SLACK_WEBHOOK_URL"},
		},
	},
	Action: func(cctx *cli.Context) error {
		ctx := context.Background()
		logger := configLogger(cctx, os.Stdout)
		configOTEL("hepa")

		dir, err := configDirectory(cctx)
		if err != nil {
			return fmt.Errorf("failed to configure identity directory: %v", err)
		}

		srv, err := NewServer(
			dir,
			Config{
				BGSHost:         cctx.String("atp-bgs-host"),
				BskyHost:        cctx.String("atp-bsky-host"),
				Logger:          logger,
				ModHost:         cctx.String("atp-mod-host"),
				ModAdminToken:   cctx.String("mod-admin-token"),
				ModUsername:     cctx.String("mod-handle"),
				ModPassword:     cctx.String("mod-password"),
				SetsFileJSON:    cctx.String("sets-json-path"),
				RedisURL:        cctx.String("redis-url"),
				SlackWebhookURL: cctx.String("slack-webhook-url"),
				HiveAPIToken:    cctx.String("hiveai-api-token"),
				AbyssHost:       cctx.String("abyss-host"),
				AbyssPassword:   cctx.String("abyss-password"),
				RatelimitBypass: cctx.String("ratelimit-bypass"),
				RulesetName:     cctx.String("ruleset"),
			},
		)
		if err != nil {
			return fmt.Errorf("failed to construct server: %v", err)
		}

		// prometheus HTTP endpoint: /metrics
		go func() {
			runtime.SetBlockProfileRate(10)
			runtime.SetMutexProfileFraction(10)
			if err := srv.RunMetrics(cctx.String("metrics-listen")); err != nil {
				slog.Error("failed to start metrics endpoint", "error", err)
				panic(fmt.Errorf("failed to start metrics endpoint: %w", err))
			}
		}()

		go func() {
			if err := srv.RunPersistCursor(ctx); err != nil {
				slog.Error("cursor routine failed", "err", err)
			}
		}()

		if srv.engine.AdminClient != nil {
			go func() {
				if err := srv.RunRefreshAdminClient(ctx); err != nil {
					slog.Error("session refresh failed", "err", err)
				}
			}()
		}

		// the main service loop
		if err := srv.RunConsumer(ctx); err != nil {
			return fmt.Errorf("failure consuming and processing firehose: %w", err)
		}
		return nil
	},
}

// for simple commands, not long-running daemons
func configEphemeralServer(cctx *cli.Context) (*Server, error) {
	// NOTE: using stderr not stdout because some commands print to stdout
	logger := configLogger(cctx, os.Stderr)

	dir, err := configDirectory(cctx)
	if err != nil {
		return nil, err
	}

	return NewServer(
		dir,
		Config{
			BGSHost:         cctx.String("atp-bgs-host"),
			BskyHost:        cctx.String("atp-bsky-host"),
			Logger:          logger,
			ModHost:         cctx.String("atp-mod-host"),
			ModAdminToken:   cctx.String("mod-admin-token"),
			ModUsername:     cctx.String("mod-handle"),
			ModPassword:     cctx.String("mod-password"),
			SetsFileJSON:    cctx.String("sets-json-path"),
			RedisURL:        cctx.String("redis-url"),
			HiveAPIToken:    cctx.String("hiveai-api-token"),
			AbyssHost:       cctx.String("abyss-host"),
			AbyssPassword:   cctx.String("abyss-password"),
			RatelimitBypass: cctx.String("ratelimit-bypass"),
			RulesetName:     cctx.String("ruleset"),
		},
	)
}

var processRecordCmd = &cli.Command{
	Name:      "process-record",
	Usage:     "process a single record in isolation",
	ArgsUsage: `<at-uri>`,
	Flags:     []cli.Flag{},
	Action: func(cctx *cli.Context) error {
		ctx := context.Background()
		uriArg := cctx.Args().First()
		if uriArg == "" {
			return fmt.Errorf("expected a single AT-URI argument")
		}
		aturi, err := syntax.ParseATURI(uriArg)
		if err != nil {
			return fmt.Errorf("not a valid AT-URI: %v", err)
		}

		srv, err := configEphemeralServer(cctx)
		if err != nil {
			return err
		}

		return capture.FetchAndProcessRecord(ctx, srv.engine, aturi)
	},
}

var processRecentCmd = &cli.Command{
	Name:      "process-recent",
	Usage:     "fetch and process recent posts for an account",
	ArgsUsage: `<at-identifier>`,
	Flags: []cli.Flag{
		&cli.IntFlag{
			Name:  "limit",
			Usage: "how many post records to parse",
			Value: 20,
		},
	},
	Action: func(cctx *cli.Context) error {
		ctx := context.Background()
		idArg := cctx.Args().First()
		if idArg == "" {
			return fmt.Errorf("expected a single AT identifier (handle or DID) argument")
		}
		atid, err := syntax.ParseAtIdentifier(idArg)
		if err != nil {
			return fmt.Errorf("not a valid handle or DID: %v", err)
		}

		srv, err := configEphemeralServer(cctx)
		if err != nil {
			return err
		}

		return capture.FetchAndProcessRecent(ctx, srv.engine, *atid, cctx.Int("limit"))
	},
}

var captureRecentCmd = &cli.Command{
	Name:      "capture-recent",
	Usage:     "fetch account metadata and recent posts for an account, dump JSON to stdout",
	ArgsUsage: `<at-identifier>`,
	Flags: []cli.Flag{
		&cli.IntFlag{
			Name:  "limit",
			Usage: "how many post records to parse",
			Value: 20,
		},
	},
	Action: func(cctx *cli.Context) error {
		ctx := context.Background()
		idArg := cctx.Args().First()
		if idArg == "" {
			return fmt.Errorf("expected a single AT identifier (handle or DID) argument")
		}
		atid, err := syntax.ParseAtIdentifier(idArg)
		if err != nil {
			return fmt.Errorf("not a valid handle or DID: %v", err)
		}

		srv, err := configEphemeralServer(cctx)
		if err != nil {
			return err
		}

		cap, err := capture.CaptureRecent(ctx, srv.engine, *atid, cctx.Int("limit"))
		if err != nil {
			return err
		}

		outJSON, err := json.MarshalIndent(cap, "", "  ")
		if err != nil {
			return err
		}

		fmt.Println(string(outJSON))
		return nil
	},
}
