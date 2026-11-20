// Command ruxbase-prepared serves the experimental prepared loopback profile.
// It does not perform schema migrations, role provisioning or public serving.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/AbdullahZaher/ruxbase/apis"
	"github.com/AbdullahZaher/ruxbase/core"

	_ "github.com/AbdullahZaher/ruxbase/migrations"
)

const manifestLimit = 4 << 20

func loadConfig(args []string, getenv func(string) string) (core.BaseAppConfig, apis.PreparedLoopbackConfig, int, error) {
	flags := flag.NewFlagSet("ruxbase-prepared", flag.ContinueOnError)
	port := flags.Int("port", 0, "explicit loopback TCP port")
	auth := flags.String("auth-collection", "", "reviewed password auth collection name")
	collections := flags.String("record-collections", "", "comma-separated reviewed base collection names")
	if err := flags.Parse(args); err != nil {
		return core.BaseAppConfig{}, apis.PreparedLoopbackConfig{}, 0, err
	}
	config := core.BaseAppConfig{
		DatabaseURL:         getenv("RUXBASE_PREPARED_DATABASE_URL"),
		LogsDatabaseURL:     getenv("RUXBASE_PREPARED_LOGS_URL"),
		PreparedRuntimeRole: getenv("RUXBASE_PREPARED_ROLE"),
		DataDir:             getenv("RUXBASE_PREPARED_DATA_DIR"),
		EncryptionEnv:       getenv("RUXBASE_PREPARED_ENCRYPTION_ENV"),
		RealtimeSource:      "hooks",
	}
	routes := apis.PreparedLoopbackConfig{AuthCollection: *auth, RecordCollections: strings.Split(*collections, ",")}
	path := getenv("RUXBASE_PREPARED_MANIFEST_FILE")
	if flags.NArg() != 0 || *port <= 0 || *port > 65535 ||
		*auth == "" || *collections == "" || path == "" ||
		config.DatabaseURL == "" || config.LogsDatabaseURL == "" ||
		config.PreparedRuntimeRole == "" || config.DataDir == "" {
		return core.BaseAppConfig{}, apis.PreparedLoopbackConfig{}, 0, errors.New("explicit port, collections, prepared role, database URLs, data directory and manifest file required")
	}
	file, err := os.Open(path)
	if err != nil {
		return core.BaseAppConfig{}, apis.PreparedLoopbackConfig{}, 0, errors.New("unable to open reviewed prepared manifest")
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, manifestLimit+1))
	if err != nil || len(raw) == 0 || len(raw) > manifestLimit {
		return core.BaseAppConfig{}, apis.PreparedLoopbackConfig{}, 0, errors.New("invalid prepared manifest file size or unreadable file")
	}
	config.PreparedManifestJSON = string(raw)
	return config, routes, *port, nil
}

func run(ctx context.Context, args []string) error {
	config, routes, port, err := loadConfig(args, os.Getenv)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
	if err != nil {
		return errors.New("unable to bind prepared loopback listener")
	}
	return apis.ServePreparedLoopback(ctx, listener, config, routes)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
