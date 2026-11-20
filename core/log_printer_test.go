package core

import (
	"context"
	"database/sql"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/AbdullahZaher/ruxbase/tools/logger"
	"github.com/pocketbase/dbx"
)

func TestBaseAppLoggerLevelDevPrint(t *testing.T) {
	testLogLevel := 4

	scenarios := []struct {
		name            string
		isDev           bool
		levels          []int
		printedLevels   []int
		persistedLevels []int
	}{
		{
			"dev mode",
			true,
			[]int{testLogLevel - 1, testLogLevel, testLogLevel + 1},
			[]int{testLogLevel - 1, testLogLevel, testLogLevel + 1},
			[]int{testLogLevel, testLogLevel + 1},
		},
		{
			"nondev mode",
			false,
			[]int{testLogLevel - 1, testLogLevel, testLogLevel + 1},
			[]int{},
			[]int{testLogLevel, testLogLevel + 1},
		},
	}

	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			const message = "logger-level-dev-print-fixture"
			var mu sync.Mutex
			var printedLevels, persistedLevels []int

			app := NewBaseApp(BaseAppConfig{
				DataDir: t.TempDir(),
				IsDev:   s.isDev,
			})
			// ruxbase: capture only this test\'s messages; cluster resync can log concurrently.
			app.logPrinter = func(log *logger.Log) {
				if log.Message == message {
					mu.Lock()
					printedLevels = append(printedLevels, int(log.Level))
					mu.Unlock()
				}
			}
			defer app.ClearBootstrap()

			if err := app.Bootstrap(); err != nil {
				t.Fatal(err)
			}

			// silence query logs
			app.concurrentDB.(*dbx.DB).ExecLogFunc = func(ctx context.Context, t time.Duration, sql string, result sql.Result, err error) {}
			app.concurrentDB.(*dbx.DB).QueryLogFunc = func(ctx context.Context, t time.Duration, sql string, rows *sql.Rows, err error) {}
			app.nonconcurrentDB.(*dbx.DB).ExecLogFunc = func(ctx context.Context, t time.Duration, sql string, result sql.Result, err error) {}
			app.nonconcurrentDB.(*dbx.DB).QueryLogFunc = func(ctx context.Context, t time.Duration, sql string, rows *sql.Rows, err error) {}

			app.Settings().Logs.MinLevel = testLogLevel
			if err := app.Save(app.Settings()); err != nil {
				t.Fatal(err)
			}

			ctx := context.WithValue(context.Background(), logger.BlockKey, true)

			// track persisted logs
			app.OnModelAfterCreateSuccess("_logs").BindFunc(func(e *ModelEvent) error {
				l, ok := e.Model.(*Log)
				if ok && l.Message == message {
					mu.Lock()
					persistedLevels = append(persistedLevels, l.Level)
					mu.Unlock()
				}
				return e.Next()
			})

			// write and persist logs
			for _, l := range s.levels {
				app.Logger().Log(ctx, slog.Level(l), message)
			}
			handler, ok := app.Logger().Handler().(*logger.BatchHandler)
			if !ok {
				t.Fatalf("Expected BatchHandler, got %v", app.Logger().Handler())
			}
			if err := handler.WriteAll(ctx); err != nil {
				t.Fatalf("Failed to write all logs: %v", err)
			}

			mu.Lock()
			defer mu.Unlock()
			slices.Sort(persistedLevels)
			slices.Sort(printedLevels)
			if !slices.Equal(s.persistedLevels, persistedLevels) {
				t.Fatalf("Expected persisted levels %v, got %v", s.persistedLevels, persistedLevels)
			}
			if !slices.Equal(s.printedLevels, printedLevels) {
				t.Fatalf("Expected printed levels %v, got %v", s.printedLevels, printedLevels)
			}
		})
	}
}
