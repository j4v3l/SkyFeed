package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/j4v3l/SkyFeed/internal/config"
	skydiscord "github.com/j4v3l/SkyFeed/internal/discord"
	"github.com/j4v3l/SkyFeed/internal/health"
	"github.com/j4v3l/SkyFeed/internal/source/readsb"
	"github.com/j4v3l/SkyFeed/internal/storage/sqlite"
	"github.com/j4v3l/SkyFeed/internal/telemetry"
)

const (
	ExitSuccess = 0
	ExitRuntime = 1
	ExitUsage   = 2
)

type CLI struct {
	Stdout    io.Writer
	Stderr    io.Writer
	LookupEnv config.LookupEnv
	ReadFile  config.ReadFile
}

func NewCLI(stdout, stderr io.Writer) CLI {
	return CLI{
		Stdout:    stdout,
		Stderr:    stderr,
		LookupEnv: os.LookupEnv,
		ReadFile:  os.ReadFile,
	}
}

func (cli CLI) Execute(ctx context.Context, args []string) int {
	if len(args) == 0 {
		cli.usage()
		return ExitUsage
	}

	switch args[0] {
	case "run":
		if len(args) != 1 {
			cli.usage()
			return ExitUsage
		}
		return cli.run(ctx)
	case "healthcheck":
		if len(args) != 1 {
			cli.usage()
			return ExitUsage
		}
		return cli.healthcheck(ctx)
	case "version":
		if len(args) != 1 {
			cli.usage()
			return ExitUsage
		}
		_, _ = fmt.Fprintln(cli.Stdout, CurrentBuild().String())
		return ExitSuccess
	case "config":
		if len(args) != 2 || args[1] != "check" {
			cli.usage()
			return ExitUsage
		}
		return cli.configCheck()
	case "migrate":
		if len(args) != 1 {
			cli.usage()
			return ExitUsage
		}
		return cli.migrate(ctx)
	case "backup":
		if len(args) != 2 {
			cli.usage()
			return ExitUsage
		}
		return cli.backup(ctx, args[1])
	case "restore":
		if len(args) != 2 {
			cli.usage()
			return ExitUsage
		}
		return cli.restore(ctx, args[1])
	case "source":
		if len(args) != 2 || args[1] != "check" {
			cli.usage()
			return ExitUsage
		}
		return cli.sourceCheck(ctx)
	case "commands":
		if len(args) != 2 || args[1] != "sync" {
			cli.usage()
			return ExitUsage
		}
		return cli.commandsSync(ctx)
	default:
		cli.usage()
		return ExitUsage
	}
}

func (cli CLI) sourceCheck(ctx context.Context) int {
	cfg, err := config.LoadWith(cli.LookupEnv, cli.ReadFile)
	if err != nil {
		_, _ = fmt.Fprintf(cli.Stderr, "configuration error: %v\n", err)
		return ExitUsage
	}
	checkContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	client := readsb.NewClient(cfg.ADSB.BaseURL, 2*time.Second)
	aircraft, aircraftErr := client.FetchAircraft(checkContext)
	receiver, receiverErr := client.FetchReceiver(checkContext)
	stats, statsErr := client.FetchStats(checkContext)
	if err := errors.Join(aircraftErr, receiverErr, statsErr); err != nil {
		_, _ = fmt.Fprintf(cli.Stderr, "source check failed: %v\n", err)
		return ExitRuntime
	}
	_, _ = fmt.Fprintf(cli.Stdout, "source valid: aircraft=%d receiver_version=%s messages=%d\n", len(aircraft.Value.Aircraft), receiver.Value.Version, stats.Value.Messages)
	return ExitSuccess
}

func (cli CLI) commandsSync(ctx context.Context) int {
	cfg, err := config.LoadWith(cli.LookupEnv, cli.ReadFile)
	if err != nil {
		_, _ = fmt.Fprintf(cli.Stderr, "configuration error: %v\n", err)
		return ExitUsage
	}
	logger, err := telemetry.NewLogger(cli.Stderr, cfg.LogFormat, cfg.LogLevel)
	if err != nil {
		_, _ = fmt.Fprintf(cli.Stderr, "logging error: %v\n", err)
		return ExitUsage
	}
	stats, err := skydiscord.RegisterCommands(ctx, cfg.Discord, logger)
	if err != nil {
		_, _ = fmt.Fprintf(cli.Stderr, "command sync failed: %v\n", err)
		return ExitRuntime
	}
	_, _ = fmt.Fprintf(cli.Stdout, "commands synchronized: created=%d updated=%d deleted=%d kept=%d ignored=%d\n", stats.Created, stats.Updated, stats.Deleted, stats.Kept, stats.Ignored)
	return ExitSuccess
}

func (cli CLI) backup(ctx context.Context, destination string) int {
	cfg, err := config.LoadWith(cli.LookupEnv, cli.ReadFile)
	if err != nil {
		_, _ = fmt.Fprintf(cli.Stderr, "configuration error: %v\n", err)
		return ExitUsage
	}
	store, err := sqlite.Open(ctx, cfg.DatabasePath)
	if err != nil {
		_, _ = fmt.Fprintf(cli.Stderr, "backup open failed: %v\n", err)
		return ExitRuntime
	}
	defer store.Close()
	if err := store.Backup(ctx, destination); err != nil {
		_, _ = fmt.Fprintf(cli.Stderr, "backup failed: %v\n", err)
		return ExitRuntime
	}
	_, _ = fmt.Fprintf(cli.Stdout, "backup created: %s\n", destination)
	return ExitSuccess
}

func (cli CLI) restore(ctx context.Context, source string) int {
	cfg, err := config.LoadWith(cli.LookupEnv, cli.ReadFile)
	if err != nil {
		_, _ = fmt.Fprintf(cli.Stderr, "configuration error: %v\n", err)
		return ExitUsage
	}
	preserved, err := sqlite.Restore(ctx, source, cfg.DatabasePath)
	if err != nil {
		_, _ = fmt.Fprintf(cli.Stderr, "restore failed: %v\n", err)
		return ExitRuntime
	}
	_, _ = fmt.Fprintf(cli.Stdout, "restore complete; previous database: %s\n", preserved)
	return ExitSuccess
}

func (cli CLI) migrate(ctx context.Context) int {
	cfg, err := config.LoadWith(cli.LookupEnv, cli.ReadFile)
	if err != nil {
		_, _ = fmt.Fprintf(cli.Stderr, "configuration error: %v\n", err)
		return ExitUsage
	}
	store, err := sqlite.Open(ctx, cfg.DatabasePath)
	if err != nil {
		_, _ = fmt.Fprintf(cli.Stderr, "migration failed: %v\n", err)
		return ExitRuntime
	}
	if err := store.Close(); err != nil {
		_, _ = fmt.Fprintf(cli.Stderr, "migration close failed: %v\n", err)
		return ExitRuntime
	}
	_, _ = fmt.Fprintln(cli.Stdout, "migrations applied")
	return ExitSuccess
}

func (cli CLI) run(ctx context.Context) int {
	cfg, err := config.LoadWith(cli.LookupEnv, cli.ReadFile)
	if err != nil {
		_, _ = fmt.Fprintf(cli.Stderr, "configuration error: %v\n", err)
		return ExitUsage
	}
	logger, err := telemetry.NewLogger(cli.Stderr, cfg.LogFormat, cfg.LogLevel)
	if err != nil {
		_, _ = fmt.Fprintf(cli.Stderr, "logging error: %v\n", err)
		return ExitUsage
	}
	if err := Run(ctx, cfg, logger); err != nil {
		logger.Error("SkyFeed stopped with an error", "component", "app", "event", "stop_error", "error", err)
		return ExitRuntime
	}
	return ExitSuccess
}

func (cli CLI) configCheck() int {
	if _, err := config.LoadWith(cli.LookupEnv, cli.ReadFile); err != nil {
		_, _ = fmt.Fprintf(cli.Stderr, "configuration error: %v\n", err)
		return ExitUsage
	}
	_, _ = fmt.Fprintln(cli.Stdout, "configuration valid")
	return ExitSuccess
}

func (cli CLI) healthcheck(ctx context.Context) int {
	address, err := config.HealthAddress(cli.LookupEnv)
	if err != nil {
		_, _ = fmt.Fprintf(cli.Stderr, "healthcheck configuration error: %v\n", err)
		return ExitUsage
	}
	checkContext, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := health.Check(checkContext, address); err != nil {
		_, _ = fmt.Fprintf(cli.Stderr, "healthcheck failed: %v\n", err)
		return ExitRuntime
	}
	return ExitSuccess
}

func (cli CLI) usage() {
	_, _ = fmt.Fprintln(cli.Stderr, "usage: skyfeed <run|healthcheck|migrate|backup PATH|restore PATH|source check|commands sync|version|config check>")
}
