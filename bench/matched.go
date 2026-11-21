package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const matchedCollection = "w00_benchmark_posts"
const matchedFields = "id,title,body,n,published"

// Both adapters time the public API and project the same record fields.
// The list response envelopes differ between the two APIs.
type matchedEndpoint struct {
	origin  string
	backend string
	anonKey string
}

func runMatched(scenario, backend string, urls []string, email, password string, records, concurrency int, duration time.Duration) (*Result, error) {
	if len(urls) != 1 || records < 1 || records > 1_000_000 || concurrency < 1 || duration <= 0 {
		return nil, errors.New("matched workload requires one origin, 1–1000000 fixture records, positive concurrency and positive duration")
	}
	endpoint, err := newMatchedEndpoint(backend, urls[0], os.Getenv("BENCH_SUPABASE_ANON_KEY"))
	if err != nil {
		return nil, err
	}
	switch scenario {
	case "matched-setup":
		if backend != "ruxbase" {
			return nil, errors.New("initialize the Supabase fixture with bench/matched-supabase.sql")
		}
		return nil, endpoint.setup(email, password, records)
	case "matched-reads", "matched-writes":
		if err := endpoint.checkFixture(records); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unknown matched scenario %q", scenario)
	}

	workerSeeds := make([]*rand.Rand, concurrency)
	for worker := range workerSeeds {
		workerSeeds[worker] = rand.New(rand.NewPCG(8, uint64(worker)))
	}

	var counter atomic.Int64
	result := load(concurrency, duration, func(worker int) error {
		if scenario == "matched-writes" {
			return endpoint.create(newPost(records + int(counter.Add(1))))
		}
		random := workerSeeds[worker]
		if random.IntN(2) == 0 {
			_, err := endpoint.read(matchedID(random.IntN(records)), 0)
			return err
		}
		_, err := endpoint.read("", random.IntN(records))
		return err
	})
	result.Target = backend
	result.Scenario = scenario
	return result, nil
}

func newMatchedEndpoint(backend, origin, anonKey string) (matchedEndpoint, error) {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Path != "" && parsed.Path != "/") || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return matchedEndpoint{}, errors.New("matched target must be an HTTP(S) loopback origin")
	}
	ip := net.ParseIP(parsed.Hostname())
	if parsed.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return matchedEndpoint{}, errors.New("matched target must be an HTTP(S) loopback origin")
	}
	switch backend {
	case "ruxbase":
		anonKey = ""
	case "supabase":
		if anonKey == "" {
			return matchedEndpoint{}, errors.New("set BENCH_SUPABASE_ANON_KEY to the disposable stack's anon key")
		}
	default:
		return matchedEndpoint{}, fmt.Errorf("unknown matched backend %q", backend)
	}
	return matchedEndpoint{strings.TrimSuffix(origin, "/"), backend, anonKey}, nil
}

func matchedID(index int) string {
	return fmt.Sprintf("b%014d", index)
}

func (endpoint matchedEndpoint) recordsURL(id string, before int, list bool) string {
	query := make(url.Values)
	if endpoint.backend == "ruxbase" {
		path := endpoint.origin + "/api/collections/" + matchedCollection + "/records"
		query.Set("fields", matchedFields)
		if id != "" {
			path += "/" + id
		}
		if list {
			query.Set("filter", fmt.Sprintf("n < %d && published = true", before))
			query.Set("sort", "-n")
			query.Set("perPage", "20")
			query.Set("skipTotal", "1")
		}
		return path + "?" + query.Encode()
	}

	query.Set("select", matchedFields)
	if id != "" {
		query.Set("id", "eq."+id)
	}
	if list {
		query.Set("n", "lt."+strconv.Itoa(before))
		query.Set("published", "eq.true")
		query.Set("order", "n.desc")
		query.Set("limit", "20")
	}
	return endpoint.origin + "/rest/v1/" + matchedCollection + "?" + query.Encode()
}

func (endpoint matchedEndpoint) request(method, target, token string, body any, expected int) ([]byte, error) {
	headers := make(http.Header)
	headers.Set("Authorization", token)
	if endpoint.backend == "supabase" {
		headers.Set("apikey", endpoint.anonKey)
		headers.Set("Prefer", "return=representation")
	}

	// Never send disposable-stack credentials to a redirect destination.
	httpClient := *client
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	status, response, err := sendRequest(&httpClient, method, target, headers, body)
	if err != nil {
		return nil, err
	}
	if status != expected {
		return nil, fmt.Errorf("%s request returned HTTP %d, expected %d", endpoint.backend, status, expected)
	}
	return response, nil
}

func (endpoint matchedEndpoint) read(id string, before int) ([]byte, error) {
	return endpoint.request(http.MethodGet, endpoint.recordsURL(id, before, id == ""), "", nil, http.StatusOK)
}

func (endpoint matchedEndpoint) create(body map[string]any) error {
	expected := http.StatusOK
	if endpoint.backend == "supabase" {
		expected = http.StatusCreated
	}
	_, err := endpoint.request(http.MethodPost, endpoint.recordsURL("", 0, false), "", body, expected)
	return err
}

func (endpoint matchedEndpoint) checkFixture(records int) error {
	for _, n := range []int{0, records - 1} {
		id := matchedID(n)
		response, err := endpoint.read(id, 0)
		if err != nil {
			return fmt.Errorf("fixture record %q: %w", id, err)
		}
		want := newPost(n)
		record := struct {
			ID        string
			Title     string
			Body      string
			N         int
			Published bool
		}{}
		if endpoint.backend == "supabase" {
			rows := []json.RawMessage{}
			if err := json.Unmarshal(response, &rows); err != nil || len(rows) != 1 {
				return fmt.Errorf("expected one Supabase fixture record %q", id)
			}
			response = rows[0]
		}
		if err := json.Unmarshal(response, &record); err != nil || record.ID != id ||
			record.Title != want["title"] || record.Body != want["body"] || record.N != n || record.Published != want["published"] {
			return fmt.Errorf("fixture record %q has unexpected content", id)
		}
	}
	return nil
}

func (endpoint matchedEndpoint) setup(email, password string, records int) error {
	response, err := endpoint.request(http.MethodPost, endpoint.origin+"/api/collections/_superusers/auth-with-password", "", map[string]string{
		"identity": email,
		"password": password,
	}, http.StatusOK)
	if err != nil {
		return err
	}
	auth := struct{ Token string }{}
	if err := json.Unmarshal(response, &auth); err != nil || auth.Token == "" {
		return errors.New("fixture setup requires a superuser token")
	}
	_, err = endpoint.request(http.MethodPost, endpoint.origin+"/api/collections", auth.Token, map[string]any{
		"name":       matchedCollection,
		"type":       "base",
		"listRule":   "",
		"viewRule":   "",
		"createRule": "",
		"fields": []map[string]string{
			{"name": "title", "type": "text"},
			{"name": "body", "type": "text"},
			{"name": "n", "type": "number"},
			{"name": "published", "type": "bool"},
		},
		"indexes": []string{"CREATE INDEX idx_w00_benchmark_posts_n ON w00_benchmark_posts (n)"},
	}, http.StatusOK)
	if err != nil {
		return fmt.Errorf("create a fresh fixture collection: %w", err)
	}
	for n := range records {
		record := newPost(n)
		record["id"] = matchedID(n)
		if err := endpoint.create(record); err != nil {
			return fmt.Errorf("seed fixture record %d: %w", n, err)
		}
	}
	return endpoint.checkFixture(records)
}
