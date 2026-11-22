// Command netproxy is a deterministic TCP proxy with HTTP-controlled latency,
// jitter, and fault-injection controls for Test G chaos experiments.
package main

import (
	"encoding/json"
	"flag"
	"io"
	"log"
	"math/rand/v2"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

type proxyConfig struct {
	LatencyMs int64 `json:"latencyMs"`
	JitterMs  int64 `json:"jitterMs"`
	DropNew   bool  `json:"dropNew"`
}

var (
	latencyNs atomic.Int64
	jitterNs  atomic.Int64
	dropNew   atomic.Bool

	connsMu sync.Mutex
	conns   = map[net.Conn]struct{}{}
)

func trackConn(c net.Conn) {
	connsMu.Lock()
	conns[c] = struct{}{}
	connsMu.Unlock()
}

func untrackConn(c net.Conn) {
	connsMu.Lock()
	delete(conns, c)
	connsMu.Unlock()
}

func cutAllConns() int {
	connsMu.Lock()
	list := make([]net.Conn, 0, len(conns))
	for c := range conns {
		list = append(list, c)
	}
	connsMu.Unlock()
	for _, c := range list {
		_ = c.Close()
	}
	return len(list)
}

func delayedCopy(dst io.Writer, src io.Reader) {
	buf := make([]byte, 32*1024)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			lat := latencyNs.Load()
			jit := jitterNs.Load()
			if lat > 0 || jit > 0 {
				d := lat
				if jit > 0 {
					d += rand.Int64N(jit*2+1) - jit
				}
				if d > 0 {
					time.Sleep(time.Duration(d))
				}
			}
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func handleConn(client net.Conn, target string) {
	defer client.Close()
	if dropNew.Load() {
		return
	}
	upstream, err := net.DialTimeout("tcp", target, 5*time.Second)
	if err != nil {
		return
	}
	defer upstream.Close()

	trackConn(client)
	trackConn(upstream)
	defer untrackConn(client)
	defer untrackConn(upstream)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		delayedCopy(upstream, client)
		if tc, ok := upstream.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
	}()
	go func() {
		defer wg.Done()
		delayedCopy(client, upstream)
		if tc, ok := client.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
	}()
	wg.Wait()
}

func main() {
	listenAddr := flag.String("listen", "0.0.0.0:5432", "TCP listen address")
	targetAddr := flag.String("target", "postgres:5432", "TCP upstream target address")
	adminAddr := flag.String("admin", "0.0.0.0:9090", "HTTP control plane address")
	flag.Parse()

	mux := http.NewServeMux()
	mux.HandleFunc("/config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var cfg proxyConfig
			if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			latencyNs.Store(cfg.LatencyMs * int64(time.Millisecond))
			jitterNs.Store(cfg.JitterMs * int64(time.Millisecond))
			dropNew.Store(cfg.DropNew)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(proxyConfig{
			LatencyMs: latencyNs.Load() / int64(time.Millisecond),
			JitterMs:  jitterNs.Load() / int64(time.Millisecond),
			DropNew:   dropNew.Load(),
		})
	})
	mux.HandleFunc("/cut", func(w http.ResponseWriter, r *http.Request) {
		closed := cutAllConns()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"closed": closed})
	})

	go func() {
		log.Printf("netproxy admin listening on %s", *adminAddr)
		if err := http.ListenAndServe(*adminAddr, mux); err != nil {
			log.Fatalf("admin server failed: %v", err)
		}
	}()

	ln, err := net.Listen("tcp", *listenAddr)
	if err != nil {
		log.Fatalf("listen %s failed: %v", *listenAddr, err)
	}
	log.Printf("netproxy forwarding %s -> %s", *listenAddr, *targetAddr)
	for {
		c, err := ln.Accept()
		if err != nil {
			continue
		}
		go handleConn(c, *targetAddr)
	}
}
