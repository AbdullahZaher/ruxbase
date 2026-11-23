package core_test

import (
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/AbdullahZaher/ruxbase/tests"
)

// ruxbase: Settings.String and Settings.DBExport must not recursively acquire
// RWMutex.RLock. A queued settings writer would otherwise deadlock a reader
// that holds the first read lock while trying to acquire the second one.
func TestSettingsConcurrentExportAndStateUpdates(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("initialize test app: %v", err)
	}
	defer app.Cleanup()

	settings := app.Settings()
	const writers = 4
	const readers = 8
	const iterations = 500

	start := make(chan struct{})
	var wg sync.WaitGroup
	errCh := make(chan error, readers)

	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < iterations; j++ {
				if j%2 == 0 {
					settings.MarkAsNew()
				} else {
					settings.MarkAsNotNew()
				}
				runtime.Gosched()
			}
		}()
	}

	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < iterations; j++ {
				exported, exportErr := settings.DBExport(app)
				if exportErr != nil {
					errCh <- fmt.Errorf("export settings: %w", exportErr)
					return
				}
				if exported["value"] == nil {
					errCh <- fmt.Errorf("export missing value")
					return
				}
				if settings.String() == "" {
					errCh <- fmt.Errorf("settings JSON unexpectedly empty")
					return
				}
				runtime.Gosched()
			}
		}()
	}

	close(start)
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("settings operations deadlocked under concurrent read/write contention")
	}

	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
}
