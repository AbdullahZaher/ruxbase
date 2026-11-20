package apis

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tools/hook"
)

var preparedCollectionName = regexp.MustCompile("^[a-zA-Z][a-zA-Z0-9_]*$")

// PreparedLoopbackConfig is a deliberately narrow, experimental local HTTP
// profile. Only password login in AuthCollection and base collection CRUD
// in RecordCollections are exposed. It is not the general Ruxbase server.
type PreparedLoopbackConfig struct {
	AuthCollection    string
	RecordCollections []string
}

func (c PreparedLoopbackConfig) validate() error {
	if !preparedCollectionName.MatchString(c.AuthCollection) || len(c.RecordCollections) == 0 {
		return errors.New("explicit prepared auth and record collections required")
	}
	seen := map[string]bool{c.AuthCollection: true}
	for _, name := range c.RecordCollections {
		if !preparedCollectionName.MatchString(name) || seen[name] {
			return errors.New("invalid or duplicate prepared record collection")
		}
		seen[name] = true
	}
	return nil
}

// ServePreparedLoopback owns a fresh prepared app and the supplied loopback
// listener. It admits the manifest before processing requests, and releases
// the pools after the server stops. Trusted callers must not expose this
// experimental listener through a reverse proxy or tunnel.
func ServePreparedLoopback(ctx context.Context, listener *net.TCPListener, config core.BaseAppConfig, routes PreparedLoopbackConfig) (err error) {
	if listener == nil {
		return errors.New("prepared loopback listener required")
	}
	defer func() { _ = listener.Close() }()
	if !listener.Addr().(*net.TCPAddr).IP.IsLoopback() {
		return errors.New("prepared HTTP requires a loopback listener")
	}
	strictEnv, _ := strconv.ParseBool(os.Getenv(core.RLSStrictEnv))
	if strictEnv || config.PreparedRuntimeRole == "" || config.PreparedManifestJSON == "" ||
		config.DatabaseURL == "" || config.LogsDatabaseURL == "" || config.DataDir == "" ||
		config.ClusterMode || config.IsDev || config.OfflineSystemPreparation ||
		config.PreprovisionedRLSRoles || config.RLSStrict || config.ReplicationIdentity.Enabled() ||
		config.RealtimeSource != "hooks" || config.DBConnect != nil {
		return errors.New("prepared HTTP requires explicit isolated runtime configuration and manifest")
	}
	if err := routes.validate(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	app := core.NewBaseApp(config)
	defer func() { err = errors.Join(err, app.ClearBootstrap()) }()
	if err := app.Bootstrap(); err != nil {
		return err
	}

	auth, err := app.FindCachedCollectionByNameOrId(routes.AuthCollection)
	if err != nil || auth == nil || auth.Name != routes.AuthCollection || auth.System ||
		!auth.IsAuth() || !auth.PasswordAuth.Enabled || auth.MFA.Enabled || auth.AuthAlert.Enabled {
		return errors.New("prepared HTTP auth collection must be a non-system password collection without MFA or mail alerts")
	}
	for _, name := range routes.RecordCollections {
		collection, err := app.FindCachedCollectionByNameOrId(name)
		if err != nil || collection == nil || collection.Name != name || collection.System || !collection.IsBase() {
			return errors.New("prepared HTTP record collection must be a non-system base collection")
		}
		for _, field := range collection.Fields {
			if _, ok := field.(*core.FileField); ok {
				return errors.New("prepared HTTP file fields are unsupported")
			}
		}
	}

	pbRouter, err := NewRouter(app)
	if err != nil {
		return err
	}
	// The standard API rules remain responsible for record authorization.
	// Limit authenticated callers to the explicitly admitted auth collection.
	pbRouter.Bind(&hook.Handler[*core.RequestEvent]{
		Id:       "ruxbasePreparedLoopbackIdentity",
		Priority: DefaultLoadAuthTokenMiddlewarePriority + 1,
		Func: func(e *core.RequestEvent) error {
			if e.Auth != nil && (e.Auth.IsSuperuser() || e.Auth.Collection().Id != auth.Id) {
				return e.ForbiddenError("Authentication collection not admitted to prepared HTTP.", nil)
			}
			return e.Next()
		},
	})
	mux, err := pbRouter.BuildMux()
	if err != nil {
		return err
	}
	handler := preparedLoopbackHandler(routes, mux)
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}
	done := make(chan error, 1)
	stop := context.AfterFunc(ctx, func() {
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
			done <- err
			return
		}
		done <- nil
	})
	err = server.Serve(listener)
	if !stop() {
		err = errors.Join(normalizePreparedServeError(err), <-done)
	}
	return normalizePreparedServeError(err)
}

func normalizePreparedServeError(err error) error {
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// preparedLoopbackHandler rejects unreviewed paths, methods and encoded
// aliases before entering the original API router.
func preparedLoopbackHandler(config PreparedLoopbackConfig, next http.Handler) http.Handler {
	allowed := make(map[string]bool, len(config.RecordCollections))
	for _, name := range config.RecordCollections {
		allowed[name] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawPath != "" || r.URL.RawQuery != "" || r.URL.ForceQuery {
			http.NotFound(w, r)
			return
		}
		parts := strings.Split(r.URL.Path, "/")
		if len(parts) < 5 || parts[0] != "" || parts[1] != "api" || parts[2] != "collections" {
			http.NotFound(w, r)
			return
		}
		name := parts[3]
		login := len(parts) == 5 && name == config.AuthCollection && parts[4] == "auth-with-password" && r.Method == http.MethodPost
		records := false
		if allowed[name] && parts[4] == "records" {
			switch len(parts) {
			case 5:
				records = r.Method == http.MethodGet || r.Method == http.MethodPost
			case 6:
				records = preparedRecordID.MatchString(parts[5])
				records = records && (r.Method == http.MethodGet || r.Method == http.MethodPatch || r.Method == http.MethodDelete)
			}
		}
		if !login && !records {
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodPost || r.Method == http.MethodPatch {
			if strings.SplitN(r.Header.Get("Content-Type"), ";", 2)[0] != "application/json" {
				http.Error(w, "JSON body required", http.StatusUnsupportedMediaType)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

var preparedRecordID = regexp.MustCompile("^[a-zA-Z0-9_]+$")
