// Command ruxbase-pprof wraps the unmodified Ruxbase application with an
// isolated diagnostic net/http/pprof listener (container-internal only) for
// Section 13 CPU, heap, goroutine, and mutex profiling during stress runs.
package main

import (
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"runtime"

	"github.com/AbdullahZaher/ruxbase"
)

func main() {
	runtime.SetMutexProfileFraction(5)
	runtime.SetBlockProfileRate(1000)

	pprofAddr := os.Getenv("STRESS_PPROF_ADDR")
	if pprofAddr == "" {
		pprofAddr = "127.0.0.1:6060"
	}
	go func() {
		log.Printf("diagnostic pprof server listening on %s", pprofAddr)
		if err := http.ListenAndServe(pprofAddr, nil); err != nil {
			log.Printf("diagnostic pprof server stopped: %v", err)
		}
	}()

	app := ruxbase.New()
	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}
