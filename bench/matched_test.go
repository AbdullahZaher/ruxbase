package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMatchedEndpointRequests(t *testing.T) {
	for _, backend := range []string{"ruxbase", "supabase"} {
		t.Run(backend, func(t *testing.T) {
			id := matchedID(0)
			fixture := newPost(0)
			fixture["id"] = id

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				query := r.URL.Query()
				if backend == "ruxbase" {
					if query.Get("fields") != matchedFields ||
						(r.URL.Path != "/api/collections/"+matchedCollection+"/records" &&
							r.URL.Path != "/api/collections/"+matchedCollection+"/records/"+id) {
						t.Error("unexpected Ruxbase route or projection")
					}
					if r.Method == http.MethodGet && r.URL.Path == "/api/collections/"+matchedCollection+"/records" {
						if query.Get("filter") != "n < 12 && published = true" ||
							query.Get("sort") != "-n" || query.Get("perPage") != "20" || query.Get("skipTotal") != "1" {
							t.Error("unexpected Ruxbase list semantics")
						}
					}
				} else {
					if query.Get("select") != matchedFields || r.URL.Path != "/rest/v1/"+matchedCollection ||
						r.Header.Get("apikey") != "example" || r.Header.Get("Authorization") != "" {
						t.Error("unexpected Supabase route, projection or authentication")
					}
					if r.Method == http.MethodGet && query.Get("id") != "eq."+id &&
						(query.Get("n") != "lt.12" || query.Get("published") != "eq.true" ||
							query.Get("order") != "n.desc" || query.Get("limit") != "20") {
						t.Error("unexpected Supabase list semantics")
					}
				}
				if r.Method == http.MethodPost {
					var record map[string]any
					if err := json.NewDecoder(r.Body).Decode(&record); err != nil || record["title"] != "Post 3" {
						t.Error("unexpected write payload")
					}
					if backend == "supabase" {
						if r.Header.Get("Prefer") != "return=representation" {
							t.Error("Supabase must return the created record")
						}
						w.WriteHeader(http.StatusCreated)
					}
				}
				var response any = fixture
				if backend == "supabase" {
					response = []map[string]any{fixture}
				}
				_ = json.NewEncoder(w).Encode(response)
			}))
			defer server.Close()

			endpoint, err := newMatchedEndpoint(backend, server.URL+"/", "example")
			if err != nil {
				t.Fatal(err)
			}
			if err := endpoint.checkFixture(1); err != nil {
				t.Fatal(err)
			}
			if _, err := endpoint.read("", 12); err != nil {
				t.Fatal(err)
			}
			if err := endpoint.create(newPost(3)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMatchedRequiresLocalOriginAndRejectsRedirect(t *testing.T) {
	for _, input := range []string{"https://example.com", "http://localhost/nonroot", "http://user@localhost", "http://localhost?key=a"} {
		if _, err := newMatchedEndpoint("ruxbase", input, ""); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
	if _, err := newMatchedEndpoint("supabase", "http://127.0.0.1:8000", ""); err == nil {
		t.Error("accepted Supabase without an anon key")
	}

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			t.Error("followed redirect")
		}
		http.Redirect(w, r, "/redirect", http.StatusFound)
	}))
	defer redirect.Close()

	endpoint, err := newMatchedEndpoint("supabase", redirect.URL, "example")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := endpoint.read(matchedID(0), 0); err == nil {
		t.Error("redirect counted as a successful request")
	}
}
