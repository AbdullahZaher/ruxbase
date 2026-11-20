package apis

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AbdullahZaher/ruxbase/core"
)

func TestPreparedLoopbackRoutes(t *testing.T) {
	config := PreparedLoopbackConfig{AuthCollection: "users", RecordCollections: []string{"items"}}
	handler := preparedLoopbackHandler(config, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, test := range []struct {
		method string
		path   string
		want   int
	}{
		{"POST", "/api/collections/users/auth-with-password", 204},
		{"GET", "/api/collections/items/records", 204},
		{"POST", "/api/collections/items/records", 204},
		{"PATCH", "/api/collections/items/records/123abc", 204},
		{"DELETE", "/api/collections/items/records/123abc", 204},
		{"GET", "/api/collections/users/records", 404},
		{"GET", "/api/settings", 404},
		{"GET", "/api/collections/_superusers/records", 404},
		{"POST", "/api/collections/users/auth-refresh", 404},
		{"GET", "/api/collections/items/records?expand=other", 404},
		{"POST", "/api/collections/items/records/123abc", 404},
		{"GET", "/api/collections/items/records/", 404},
	} {
		req := httptest.NewRequest(test.method, test.path, nil)
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		if res.Code != test.want {
			t.Errorf("%s %s: got %d, want %d", test.method, test.path, res.Code, test.want)
		}
	}
}

func TestPreparedLoopbackRequiresManifest(t *testing.T) {
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := ServePreparedLoopback(context.Background(), listener, core.BaseAppConfig{}, PreparedLoopbackConfig{}); err == nil {
		t.Fatal("missing explicit runtime configuration admitted")
	}
}
