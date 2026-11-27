// Package journeys implements realistic customer journeys (Journeys A–H)
// for the Ruxbase W11-05 168-hour staging soak, combining open-loop arrival pacing
// (preventing coordinated omission) with stateful closed-loop journeys and
// persistent SSE realtime subscriptions.
package journeys

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AbdullahZaher/ruxbase/bench/soak/internal/state"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	SoakItemsCollection   = "soak_items"
	SoakVectorsCollection = "soak_vectors"
	DefaultTenantCount    = 12
	UsersPerTenant        = 2
	SyntheticUserPassword = "SoakSyntheticUser!2026"
)

// SyntheticUser represents an authenticated synthetic tenant user.
type SyntheticUser struct {
	TenantID string `json:"tenantId"`
	UserID   string `json:"userId"`
	Email    string `json:"email"`
	Password string `json:"password"`
	Token    string `json:"token"`
}

// Fixtures holds active staging synthetic tenant/user tokens and seed record IDs.
type Fixtures struct {
	mu             sync.RWMutex
	SuperuserEmail string            `json:"superuserEmail"`
	SuperuserPass  string            `json:"-"`
	SuperuserToken string            `json:"-"`
	Users          []SyntheticUser   `json:"users"`
	SeedIDsByTen   map[string]string `json:"seedIdsByTenant"`
	VectorDims     int               `json:"vectorDims"`
}

// OpKind categorizes every executed customer operation.
type OpKind string

const (
	OpAuthLifecycle   OpKind = "journey_a_auth"
	OpCRUDRead        OpKind = "journey_b_crud_read"
	OpCRUDWrite       OpKind = "journey_b_crud_write"
	OpCRUDUpdate      OpKind = "journey_b_crud_update"
	OpCRUDDelete      OpKind = "journey_b_crud_delete"
	OpCrossTenantDeny OpKind = "journey_c_rls_negative"
	OpRealtimeSync    OpKind = "journey_d_realtime_sync"
	OpFileUpload      OpKind = "journey_f_file_upload"
	OpDBQuerySearch   OpKind = "journey_g_db_query"
	OpVectorSearch    OpKind = "journey_g_vector_search"
)

// OpOutcome distinguishes HTTP requests, mutations, journeys, no-ops, and failure attribution.
type OpOutcome struct {
	Kind               OpKind
	StatusCode         int
	Latency            time.Duration
	HTTPAttempted      int
	HTTPSucceeded      int
	MutationsAttempted int
	MutationsCommitted int
	NoOpSkipped        int
	AppFailure         bool
	GenFailure         bool
	LedgerPersistErr   bool
	Timeout            bool
	CrossTenantChk     bool
	CrossTenantLk      bool
	IntegrityErr       bool
	Err                error
}

// CanonicalRecordChecksum defines the single canonical SHA-256 checksum across both
// record creation and record updates: sha256(opID + "|" + title).
func CanonicalRecordChecksum(opID, title string) string {
	return sha256Hex(opID + "|" + title)
}

// NewHTTPClient returns a tuned HTTP client for sustained 7-day load generation.
func NewHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	dialer := &net.Dialer{
		Timeout:   5 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	tr := &http.Transport{
		DialContext:         dialer.DialContext,
		MaxIdleConns:        512,
		MaxIdleConnsPerHost: 512,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 5 * time.Second,
		ForceAttemptHTTP2:   false,
	}
	return &http.Client{
		Transport: tr,
		Timeout:   timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// DoJSON sends a JSON HTTP request and returns status code and response body.
func DoJSON(ctx context.Context, client *http.Client, method, target, token string, body any) (int, []byte, error) {
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, rdr)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	return resp.StatusCode, raw, err
}

// AuthSuperuser authenticates the staging superuser for ops/setup APIs.
func AuthSuperuser(ctx context.Context, client *http.Client, baseURL, email, password string) (string, error) {
	status, raw, err := DoJSON(ctx, client, http.MethodPost, strings.TrimRight(baseURL, "/")+"/api/collections/_superusers/auth-with-password", "", map[string]string{
		"identity": email,
		"password": password,
	})
	if err != nil || status != http.StatusOK {
		return "", fmt.Errorf("superuser auth failed: status=%d err=%v", status, err)
	}
	var resp struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil || resp.Token == "" {
		return "", fmt.Errorf("decode superuser token: %w", err)
	}
	return resp.Token, nil
}

// EnsureFixtures idempotently creates the staging synthetic collections, tenants, users,
// and seed records without wiping prior campaign data if resuming.
func EnsureFixtures(ctx context.Context, client *http.Client, baseURL, superEmail, superPass string, tenantCount int) (*Fixtures, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	if tenantCount <= 0 {
		tenantCount = DefaultTenantCount
	}
	token, err := AuthSuperuser(ctx, client, baseURL, superEmail, superPass)
	if err != nil {
		return nil, err
	}

	// 1. Ensure tenant_id field exists on users collection
	st, raw, err := DoJSON(ctx, client, http.MethodGet, baseURL+"/api/collections/users", token, nil)
	if err != nil || st != http.StatusOK {
		return nil, fmt.Errorf("get users collection: status=%d err=%v", st, err)
	}
	var usersCol map[string]any
	if err := json.Unmarshal(raw, &usersCol); err != nil {
		return nil, err
	}
	fields, _ := usersCol["fields"].([]any)
	hasTenant := false
	for _, f := range fields {
		if fm, ok := f.(map[string]any); ok && fm["name"] == "tenant_id" {
			hasTenant = true
			break
		}
	}
	if !hasTenant {
		fields = append(fields, map[string]any{
			"name":     "tenant_id",
			"type":     "text",
			"required": false,
		})
		st, _, err = DoJSON(ctx, client, http.MethodPatch, baseURL+"/api/collections/users", token, map[string]any{
			"fields": fields,
		})
		if err != nil || st != http.StatusOK {
			return nil, fmt.Errorf("patch users collection with tenant_id: status=%d err=%v", st, err)
		}
	}

	// 2. Ensure soak_items multi-tenant RLS collection exists
	rlsRule := "tenant_id = @request.auth.tenant_id"
	createRule := `@request.auth.id != "" && tenant_id = @request.auth.tenant_id`
	st, _, _ = DoJSON(ctx, client, http.MethodGet, baseURL+"/api/collections/"+SoakItemsCollection, token, nil)
	if st != http.StatusOK {
		st, body, err := DoJSON(ctx, client, http.MethodPost, baseURL+"/api/collections", token, map[string]any{
			"name":       SoakItemsCollection,
			"type":       "base",
			"listRule":   rlsRule,
			"viewRule":   rlsRule,
			"createRule": createRule,
			"updateRule": rlsRule,
			"deleteRule": rlsRule,
			"fields": []map[string]any{
				{"name": "tenant_id", "type": "text", "required": true},
				{"name": "owner", "type": "text"},
				{"name": "op_id", "type": "text"},
				{"name": "category", "type": "text"},
				{"name": "seq", "type": "number"},
				{"name": "amount", "type": "number"},
				{"name": "title", "type": "text"},
				{"name": "body", "type": "text"},
				{"name": "checksum", "type": "text"},
				{"name": "attachment", "type": "file", "maxSelect": 1, "maxSize": 10 * 1024 * 1024},
				{"name": "created", "type": "autodate", "onCreate": true},
				{"name": "updated", "type": "autodate", "onCreate": true, "onUpdate": true},
			},
			"indexes": []string{
				"CREATE INDEX idx_soak_items_tenant_seq ON soak_items (tenant_id, seq DESC)",
				"CREATE INDEX idx_soak_items_tenant_cat ON soak_items (tenant_id, category)",
				"CREATE INDEX idx_soak_items_op_id ON soak_items (op_id)",
			},
		})
		if err != nil || st != http.StatusOK {
			return nil, fmt.Errorf("create %s: status=%d err=%v body=%s", SoakItemsCollection, st, err, string(body))
		}
	}

	// 3. Ensure soak_vectors pgvector collection exists (128-dim cosine)
	const vecDims = 128
	st, _, _ = DoJSON(ctx, client, http.MethodGet, baseURL+"/api/collections/"+SoakVectorsCollection, token, nil)
	if st != http.StatusOK {
		st, body, err := DoJSON(ctx, client, http.MethodPost, baseURL+"/api/collections", token, map[string]any{
			"name":       SoakVectorsCollection,
			"type":       "base",
			"listRule":   rlsRule,
			"viewRule":   rlsRule,
			"createRule": createRule,
			"updateRule": rlsRule,
			"deleteRule": rlsRule,
			"fields": []map[string]any{
				{"name": "tenant_id", "type": "text", "required": true},
				{"name": "label", "type": "text"},
				{"name": "embedding", "type": "vector", "dimensions": vecDims, "metric": "cosine", "index": true},
			},
			"indexes": []string{
				"CREATE INDEX idx_soak_vectors_tenant ON soak_vectors (tenant_id)",
			},
		})
		if err != nil || st != http.StatusOK {
			return nil, fmt.Errorf("create %s: status=%d err=%v body=%s", SoakVectorsCollection, st, err, string(body))
		}
	}

	// 4. Create or authenticate synthetic multi-tenant users
	users := make([]SyntheticUser, 0, tenantCount*UsersPerTenant)
	seedIDs := make(map[string]string, tenantCount)
	for t := range tenantCount {
		tid := fmt.Sprintf("soak_tenant_%02d", t)
		for u := range UsersPerTenant {
			email := fmt.Sprintf("soak_t%02d_u%02d@staging.invalid", t, u)
			stCreate, bodyCreate, errCreate := DoJSON(ctx, client, http.MethodPost, baseURL+"/api/collections/users/records", token, map[string]any{
				"email":           email,
				"password":        SyntheticUserPassword,
				"passwordConfirm": SyntheticUserPassword,
				"verified":        true,
				"tenant_id":       tid,
			})
			if errCreate != nil {
				return nil, fmt.Errorf("create synthetic user %s: %w", email, errCreate)
			}
			if stCreate != http.StatusOK && !strings.Contains(string(bodyCreate), "validation_not_unique") {
				return nil, fmt.Errorf("create synthetic user %s: status=%d body=%s", email, stCreate, string(bodyCreate))
			}
			st, body, err := DoJSON(ctx, client, http.MethodPost, baseURL+"/api/collections/users/auth-with-password", "", map[string]string{
				"identity": email,
				"password": SyntheticUserPassword,
			})
			if err != nil || st != http.StatusOK {
				return nil, fmt.Errorf("auth synthetic user %s: status=%d err=%v", email, st, err)
			}
			var resp struct {
				Token  string `json:"token"`
				Record struct {
					ID       string `json:"id"`
					TenantID string `json:"tenant_id"`
				} `json:"record"`
			}
			if err := json.Unmarshal(body, &resp); err != nil || resp.Token == "" {
				return nil, fmt.Errorf("decode user %s auth response: %w", email, err)
			}
			if resp.Record.TenantID != tid {
				_, _, _ = DoJSON(ctx, client, http.MethodPatch, baseURL+"/api/collections/users/records/"+resp.Record.ID, token, map[string]any{
					"tenant_id": tid,
				})
			}
			users = append(users, SyntheticUser{
				TenantID: tid,
				UserID:   resp.Record.ID,
				Email:    email,
				Password: SyntheticUserPassword,
				Token:    resp.Token,
			})
		}

		// Ensure at least one baseline record per tenant for negative cross-tenant tests
		primary := users[len(users)-UsersPerTenant]
		st, body, err := DoJSON(ctx, client, http.MethodGet, baseURL+"/api/collections/"+SoakItemsCollection+"/records?perPage=1", primary.Token, nil)
		if err == nil && st == http.StatusOK {
			var lr struct {
				Items []struct {
					ID string `json:"id"`
				} `json:"items"`
			}
			if json.Unmarshal(body, &lr) == nil && len(lr.Items) > 0 {
				seedIDs[tid] = lr.Items[0].ID
			}
		}
		if seedIDs[tid] == "" {
			seedOp := fmt.Sprintf("seed-%s", tid)
			seedTitle := fmt.Sprintf("Baseline Seed %s", tid)
			st, body, err := DoJSON(ctx, client, http.MethodPost, baseURL+"/api/collections/"+SoakItemsCollection+"/records", primary.Token, map[string]any{
				"tenant_id": tid,
				"owner":     primary.UserID,
				"op_id":     seedOp,
				"category":  "cat_seed",
				"seq":       1,
				"amount":    10.5,
				"title":     seedTitle,
				"body":      "Synthetic staging seed record",
				"checksum":  CanonicalRecordChecksum(seedOp, seedTitle),
			})
			if err != nil || st != http.StatusOK {
				return nil, fmt.Errorf("seed record for %s: status=%d err=%v", tid, st, err)
			}
			var created struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(body, &created) == nil {
				seedIDs[tid] = created.ID
			}
			_, _, _ = DoJSON(ctx, client, http.MethodPost, baseURL+"/api/collections/"+SoakVectorsCollection+"/records", primary.Token, map[string]any{
				"tenant_id": tid,
				"label":     "seed_" + tid,
				"embedding": MakeVector(t+1, vecDims),
			})
		}
	}

	return &Fixtures{
		SuperuserEmail: superEmail,
		SuperuserPass:  superPass,
		SuperuserToken: token,
		Users:          users,
		SeedIDsByTen:   seedIDs,
		VectorDims:     vecDims,
	}, nil
}

// RefreshTokens refreshes superuser and synthetic user JWTs so tokens never expire during a 168h run.
func (f *Fixtures) RefreshTokens(ctx context.Context, client *http.Client, baseURL string) error {
	baseURL = strings.TrimRight(baseURL, "/")
	tok, err := AuthSuperuser(ctx, client, baseURL, f.SuperuserEmail, f.SuperuserPass)
	if err != nil {
		return err
	}
	f.mu.Lock()
	f.SuperuserToken = tok
	f.mu.Unlock()

	f.mu.RLock()
	n := len(f.Users)
	f.mu.RUnlock()
	for i := range n {
		f.mu.RLock()
		u := f.Users[i]
		f.mu.RUnlock()
		st, raw, err := DoJSON(ctx, client, http.MethodPost, baseURL+"/api/collections/users/auth-refresh", u.Token, nil)
		if err != nil || st != http.StatusOK {
			st, raw, err = DoJSON(ctx, client, http.MethodPost, baseURL+"/api/collections/users/auth-with-password", "", map[string]string{
				"identity": u.Email,
				"password": u.Password,
			})
		}
		if err == nil && st == http.StatusOK {
			var resp struct {
				Token string `json:"token"`
			}
			if json.Unmarshal(raw, &resp) == nil && resp.Token != "" {
				f.mu.Lock()
				f.Users[i].Token = resp.Token
				f.mu.Unlock()
			}
		}
	}
	return nil
}

// SuperToken returns the current superuser JWT safely.
func (f *Fixtures) SuperToken() string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.SuperuserToken
}

// UserForTenant returns an authenticated synthetic user belonging to tenantID.
func (f *Fixtures) UserForTenant(tenantID string) (SyntheticUser, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	for _, u := range f.Users {
		if u.TenantID == tenantID {
			return u, true
		}
	}
	return SyntheticUser{}, false
}

// ForeignUserForTenant returns an authenticated synthetic user belonging to a different tenant.
func (f *Fixtures) ForeignUserForTenant(tenantID string) (SyntheticUser, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	for _, u := range f.Users {
		if u.TenantID != tenantID {
			return u, true
		}
	}
	return SyntheticUser{}, false
}

// FetchTenantRLSToken obtains a short-lived cryptographic RLS JWT for direct PostgreSQL
// ruxbase_rls.set_claims verification via POST /api/rls/token.
func FetchTenantRLSToken(ctx context.Context, client *http.Client, baseURL, userJWT string, durationSec int64) (string, error) {
	if durationSec < 60 {
		durationSec = 60
	}
	st, raw, err := DoJSON(ctx, client, http.MethodPost, strings.TrimRight(baseURL, "/")+"/api/rls/token", userJWT, map[string]any{
		"duration": durationSec,
	})
	if err != nil || st != http.StatusOK {
		return "", fmt.Errorf("fetch /api/rls/token: status=%d err=%v", st, err)
	}
	var resp struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil || resp.Token == "" {
		return "", errors.New("empty RLS token response")
	}
	return resp.Token, nil
}

// Runner executes mixed synthetic customer journeys against Ruxbase.
type Runner struct {
	client   *http.Client
	baseURL  string
	fixtures *Fixtures
	store    *state.Store
	seq      atomic.Int64

	// recentIDs is an exclusive checkout pool: an ID checked out for update or delete
	// is removed from the pool until update completes, preventing concurrent write races.
	recentMu  sync.Mutex
	recentIDs map[string][]string
}

// NewRunner constructs a customer-journey runner backed by the durable campaign store.
func NewRunner(client *http.Client, baseURL string, fixtures *Fixtures, store *state.Store) *Runner {
	return &Runner{
		client:    client,
		baseURL:   strings.TrimRight(baseURL, "/"),
		fixtures:  fixtures,
		store:     store,
		recentIDs: make(map[string][]string),
	}
}

// ExecuteMixed dispatches a realistic customer journey operation according to writeRatio.
func (r *Runner) ExecuteMixed(ctx context.Context, rng *rand.Rand, writeRatio float64) OpOutcome {
	r.fixtures.mu.RLock()
	if len(r.fixtures.Users) == 0 {
		r.fixtures.mu.RUnlock()
		return OpOutcome{Kind: OpCRUDRead, GenFailure: true, Err: errors.New("no synthetic users loaded")}
	}
	uIdx := rng.IntN(len(r.fixtures.Users))
	user := r.fixtures.Users[uIdx]
	otherIdx := (uIdx + UsersPerTenant) % len(r.fixtures.Users)
	other := r.fixtures.Users[otherIdx]
	foreignSeedID := r.fixtures.SeedIDsByTen[other.TenantID]
	r.fixtures.mu.RUnlock()

	roll := rng.Float64()
	switch {
	case roll < 0.05:
		return r.runCrossTenantNegative(ctx, user, other.TenantID, foreignSeedID)
	case roll < 0.09:
		return r.runAuthJourney(ctx, user)
	case roll < 0.12:
		return r.runFileStorageJourney(ctx, user)
	case roll < 0.16:
		return r.runVectorSearch(ctx, user, rng)
	case roll < 0.19:
		return r.runRealtimeSync(ctx, user)
	case roll < 0.19+writeRatio:
		wRoll := rng.Float64()
		switch {
		case wRoll < 0.65:
			return r.runCRUDCreate(ctx, user, rng)
		case wRoll < 0.90:
			return r.runCRUDUpdate(ctx, user, rng)
		default:
			return r.runCRUDDelete(ctx, user, rng)
		}
	default:
		return r.runCRUDListRead(ctx, user, rng)
	}
}

func (r *Runner) runAuthJourney(ctx context.Context, u SyntheticUser) OpOutcome {
	t0 := time.Now()
	st, raw, err := DoJSON(ctx, r.client, http.MethodPost, r.baseURL+"/api/collections/users/auth-refresh", u.Token, nil)
	if err != nil {
		return classifyNetErr(OpAuthLifecycle, time.Since(t0), 1, 0, err)
	}
	if st != http.StatusOK {
		return OpOutcome{Kind: OpAuthLifecycle, StatusCode: st, Latency: time.Since(t0), HTTPAttempted: 1, AppFailure: true, Err: fmt.Errorf("auth-refresh status %d", st)}
	}
	var resp struct {
		Token string `json:"token"`
	}
	if json.Unmarshal(raw, &resp) != nil || resp.Token == "" {
		return OpOutcome{Kind: OpAuthLifecycle, StatusCode: st, Latency: time.Since(t0), HTTPAttempted: 1, IntegrityErr: true, AppFailure: true, Err: errors.New("empty refreshed token")}
	}
	st2, _, err := DoJSON(ctx, r.client, http.MethodGet, r.baseURL+"/api/collections/users/records/"+u.UserID, resp.Token, nil)
	if err != nil {
		return classifyNetErr(OpAuthLifecycle, time.Since(t0), 2, 1, err)
	}
	if st2 != http.StatusOK {
		return OpOutcome{Kind: OpAuthLifecycle, StatusCode: st2, Latency: time.Since(t0), HTTPAttempted: 2, HTTPSucceeded: 1, AppFailure: true, Err: fmt.Errorf("profile read status %d", st2)}
	}
	return OpOutcome{Kind: OpAuthLifecycle, StatusCode: st2, Latency: time.Since(t0), HTTPAttempted: 2, HTTPSucceeded: 2}
}

func (r *Runner) recordIntent(entry state.LedgerEntry) error {
	if r.store == nil {
		return nil
	}
	entry.Phase = "intent"
	entry.TimestampUTC = time.Now().UTC().Format(time.RFC3339Nano)
	_, err := r.store.AppendLedger(entry)
	return err
}

func (r *Runner) recordAck(entry state.LedgerEntry, committed bool, httpStatus int, err error) error {
	if r.store == nil {
		return nil
	}
	entry.Phase = "ack"
	entry.TimestampUTC = time.Now().UTC().Format(time.RFC3339Nano)
	entry.HTTPStatus = httpStatus
	entry.Committed = &committed
	if err != nil {
		entry.Error = err.Error()
	}
	_, persistErr := r.store.AppendLedger(entry)
	return persistErr
}

func (r *Runner) runCRUDCreate(ctx context.Context, u SyntheticUser, rng *rand.Rand) OpOutcome {
	t0 := time.Now()
	seq := r.seq.Add(1)
	opID := fmt.Sprintf("op-%d-%d", time.Now().UTC().UnixNano(), seq)
	title := fmt.Sprintf("Soak Record %s #%d", u.TenantID, seq)
	chk := CanonicalRecordChecksum(opID, title)
	cat := fmt.Sprintf("cat_%d", rng.IntN(8))

	intent := state.LedgerEntry{
		OpID:         opID,
		Kind:         "record_create",
		TenantID:     u.TenantID,
		Collection:   SoakItemsCollection,
		ExpectedHash: chk,
		ExpectedText: title,
	}
	if err := r.recordIntent(intent); err != nil {
		return OpOutcome{Kind: OpCRUDWrite, Latency: time.Since(t0), LedgerPersistErr: true, IntegrityErr: true, GenFailure: true, Err: err}
	}

	st, raw, err := DoJSON(ctx, r.client, http.MethodPost, r.baseURL+"/api/collections/"+SoakItemsCollection+"/records", u.Token, map[string]any{
		"tenant_id": u.TenantID,
		"owner":     u.UserID,
		"op_id":     opID,
		"category":  cat,
		"seq":       seq,
		"amount":    math.Round((rng.Float64()*999.0+1.0)*100) / 100,
		"title":     title,
		"body":      fmt.Sprintf("Synthetic payload for %s op=%s", u.TenantID, opID),
		"checksum":  chk,
	})
	if err != nil {
		if ackErr := r.recordAck(intent, false, 0, err); ackErr != nil {
			return OpOutcome{Kind: OpCRUDWrite, Latency: time.Since(t0), HTTPAttempted: 1, MutationsAttempted: 1, LedgerPersistErr: true, IntegrityErr: true, GenFailure: true, Err: ackErr}
		}
		out := classifyNetErr(OpCRUDWrite, time.Since(t0), 1, 0, err)
		out.MutationsAttempted = 1
		return out
	}
	if st != http.StatusOK {
		errMsg := fmt.Errorf("create status %d", st)
		if ackErr := r.recordAck(intent, false, st, errMsg); ackErr != nil {
			return OpOutcome{Kind: OpCRUDWrite, StatusCode: st, Latency: time.Since(t0), HTTPAttempted: 1, MutationsAttempted: 1, LedgerPersistErr: true, IntegrityErr: true, GenFailure: true, Err: ackErr}
		}
		return OpOutcome{Kind: OpCRUDWrite, StatusCode: st, Latency: time.Since(t0), HTTPAttempted: 1, MutationsAttempted: 1, AppFailure: true, Err: errMsg}
	}

	var rec struct {
		ID       string `json:"id"`
		TenantID string `json:"tenant_id"`
	}
	if json.Unmarshal(raw, &rec) != nil || rec.ID == "" {
		decErr := errors.New("missing created id")
		ackErr := r.recordAck(intent, false, st, decErr)
		return OpOutcome{Kind: OpCRUDWrite, StatusCode: st, Latency: time.Since(t0), HTTPAttempted: 1, MutationsAttempted: 1, LedgerPersistErr: ackErr != nil, IntegrityErr: true, AppFailure: ackErr == nil, GenFailure: ackErr != nil, Err: errors.Join(decErr, ackErr)}
	}
	if rec.TenantID != u.TenantID {
		leakErr := fmt.Errorf("tenant mismatch on create: got %s want %s", rec.TenantID, u.TenantID)
		ackErr := r.recordAck(intent, false, st, leakErr)
		return OpOutcome{Kind: OpCRUDWrite, StatusCode: st, Latency: time.Since(t0), HTTPAttempted: 1, MutationsAttempted: 1, LedgerPersistErr: ackErr != nil, CrossTenantLk: true, IntegrityErr: true, AppFailure: true, GenFailure: ackErr != nil, Err: errors.Join(leakErr, ackErr)}
	}

	intent.EntityID = rec.ID
	if ackErr := r.recordAck(intent, true, st, nil); ackErr != nil {
		return OpOutcome{Kind: OpCRUDWrite, StatusCode: st, Latency: time.Since(t0), HTTPAttempted: 1, HTTPSucceeded: 1, MutationsAttempted: 1, MutationsCommitted: 1, LedgerPersistErr: true, IntegrityErr: true, GenFailure: true, Err: ackErr}
	}

	r.returnID(u.TenantID, rec.ID)
	return OpOutcome{Kind: OpCRUDWrite, StatusCode: st, Latency: time.Since(t0), HTTPAttempted: 1, HTTPSucceeded: 1, MutationsAttempted: 1, MutationsCommitted: 1}
}

func (r *Runner) runCRUDUpdate(ctx context.Context, u SyntheticUser, rng *rand.Rand) OpOutcome {
	id := r.checkoutID(u.TenantID)
	if id == "" {
		// Never silently fake a no-op; perform a real create instead.
		return r.runCRUDCreate(ctx, u, rng)
	}
	t0 := time.Now()
	seq := r.seq.Add(1)
	opID := fmt.Sprintf("upd-%d-%d", time.Now().UTC().UnixNano(), seq)
	title := fmt.Sprintf("Updated Soak %s #%d", u.TenantID, seq)
	chk := CanonicalRecordChecksum(opID, title)

	intent := state.LedgerEntry{
		OpID:         opID,
		Kind:         "record_update",
		TenantID:     u.TenantID,
		Collection:   SoakItemsCollection,
		EntityID:     id,
		ExpectedHash: chk,
		ExpectedText: title,
	}
	if err := r.recordIntent(intent); err != nil {
		r.returnID(u.TenantID, id)
		return OpOutcome{Kind: OpCRUDUpdate, Latency: time.Since(t0), LedgerPersistErr: true, IntegrityErr: true, GenFailure: true, Err: err}
	}

	st, _, err := DoJSON(ctx, r.client, http.MethodPatch, r.baseURL+"/api/collections/"+SoakItemsCollection+"/records/"+id, u.Token, map[string]any{
		"op_id":    opID,
		"title":    title,
		"checksum": chk,
	})
	if err != nil {
		// Record non-committed ack on transport/error paths; do not return id blindly while server state is uncertain.
		ackErr := r.recordAck(intent, false, st, err)
		out := classifyNetErr(OpCRUDUpdate, time.Since(t0), 1, 0, err)
		out.MutationsAttempted = 1
		if ackErr != nil {
			out.LedgerPersistErr = true
			out.IntegrityErr = true
			out.GenFailure = true
			out.Err = errors.Join(err, ackErr)
		}
		return out
	}
	if st != http.StatusOK {
		errMsg := fmt.Errorf("update status %d", st)
		ackErr := r.recordAck(intent, false, st, errMsg)
		if st != http.StatusNotFound {
			r.returnID(u.TenantID, id)
		}
		if ackErr != nil {
			return OpOutcome{Kind: OpCRUDUpdate, StatusCode: st, Latency: time.Since(t0), HTTPAttempted: 1, MutationsAttempted: 1, LedgerPersistErr: true, IntegrityErr: true, GenFailure: true, Err: errors.Join(errMsg, ackErr)}
		}
		return OpOutcome{Kind: OpCRUDUpdate, StatusCode: st, Latency: time.Since(t0), HTTPAttempted: 1, MutationsAttempted: 1, AppFailure: true, Err: errMsg}
	}

	if ackErr := r.recordAck(intent, true, st, nil); ackErr != nil {
		r.returnID(u.TenantID, id)
		return OpOutcome{Kind: OpCRUDUpdate, StatusCode: st, Latency: time.Since(t0), HTTPAttempted: 1, HTTPSucceeded: 1, MutationsAttempted: 1, MutationsCommitted: 1, LedgerPersistErr: true, IntegrityErr: true, GenFailure: true, Err: ackErr}
	}
	r.returnID(u.TenantID, id)
	return OpOutcome{Kind: OpCRUDUpdate, StatusCode: st, Latency: time.Since(t0), HTTPAttempted: 1, HTTPSucceeded: 1, MutationsAttempted: 1, MutationsCommitted: 1}
}

func (r *Runner) runCRUDDelete(ctx context.Context, u SyntheticUser, rng *rand.Rand) OpOutcome {
	id := r.checkoutID(u.TenantID)
	if id == "" {
		// Never fabricate a fake 204 NoContent no-op; run a real list read instead.
		out := r.runCRUDListRead(ctx, u, rng)
		out.NoOpSkipped = 1
		return out
	}
	t0 := time.Now()
	seq := r.seq.Add(1)
	opID := fmt.Sprintf("del-%d-%d", time.Now().UTC().UnixNano(), seq)

	intent := state.LedgerEntry{
		OpID:       opID,
		Kind:       "record_delete",
		TenantID:   u.TenantID,
		Collection: SoakItemsCollection,
		EntityID:   id,
		Deleted:    true,
	}
	if err := r.recordIntent(intent); err != nil {
		r.returnID(u.TenantID, id)
		return OpOutcome{Kind: OpCRUDDelete, Latency: time.Since(t0), LedgerPersistErr: true, IntegrityErr: true, GenFailure: true, Err: err}
	}

	st, _, err := DoJSON(ctx, r.client, http.MethodDelete, r.baseURL+"/api/collections/"+SoakItemsCollection+"/records/"+id, u.Token, nil)
	if err != nil {
		ackErr := r.recordAck(intent, false, 0, err)
		out := classifyNetErr(OpCRUDDelete, time.Since(t0), 1, 0, err)
		out.MutationsAttempted = 1
		if ackErr != nil {
			out.LedgerPersistErr = true
			out.IntegrityErr = true
			out.GenFailure = true
			out.Err = errors.Join(err, ackErr)
		}
		return out
	}
	if st != http.StatusNoContent {
		errMsg := fmt.Errorf("delete status %d", st)
		if ackErr := r.recordAck(intent, false, st, errMsg); ackErr != nil {
			return OpOutcome{Kind: OpCRUDDelete, StatusCode: st, Latency: time.Since(t0), HTTPAttempted: 1, MutationsAttempted: 1, LedgerPersistErr: true, IntegrityErr: true, GenFailure: true, Err: errors.Join(errMsg, ackErr)}
		}
		return OpOutcome{Kind: OpCRUDDelete, StatusCode: st, Latency: time.Since(t0), HTTPAttempted: 1, MutationsAttempted: 1, AppFailure: true, Err: errMsg}
	}

	if ackErr := r.recordAck(intent, true, st, nil); ackErr != nil {
		return OpOutcome{Kind: OpCRUDDelete, StatusCode: st, Latency: time.Since(t0), HTTPAttempted: 1, HTTPSucceeded: 1, MutationsAttempted: 1, MutationsCommitted: 1, LedgerPersistErr: true, IntegrityErr: true, GenFailure: true, Err: ackErr}
	}
	return OpOutcome{Kind: OpCRUDDelete, StatusCode: st, Latency: time.Since(t0), HTTPAttempted: 1, HTTPSucceeded: 1, MutationsAttempted: 1, MutationsCommitted: 1}
}

func (r *Runner) runCRUDListRead(ctx context.Context, u SyntheticUser, rng *rand.Rand) OpOutcome {
	t0 := time.Now()
	cat := fmt.Sprintf("cat_%d", rng.IntN(8))
	q := url.Values{}
	q.Set("page", "1")
	q.Set("perPage", "20")
	q.Set("sort", "-seq")
	q.Set("filter", fmt.Sprintf("category='%s'", cat))

	st, raw, err := DoJSON(ctx, r.client, http.MethodGet, r.baseURL+"/api/collections/"+SoakItemsCollection+"/records?"+q.Encode(), u.Token, nil)
	if err != nil {
		return classifyNetErr(OpCRUDRead, time.Since(t0), 1, 0, err)
	}
	if st != http.StatusOK {
		return OpOutcome{Kind: OpCRUDRead, StatusCode: st, Latency: time.Since(t0), HTTPAttempted: 1, AppFailure: true, Err: fmt.Errorf("list status %d", st)}
	}
	var lr struct {
		Items []struct {
			TenantID string `json:"tenant_id"`
		} `json:"items"`
	}
	if json.Unmarshal(raw, &lr) != nil {
		return OpOutcome{Kind: OpCRUDRead, StatusCode: st, Latency: time.Since(t0), HTTPAttempted: 1, IntegrityErr: true, AppFailure: true, Err: errors.New("invalid list JSON")}
	}
	for _, it := range lr.Items {
		if it.TenantID != u.TenantID {
			return OpOutcome{Kind: OpCRUDRead, StatusCode: st, Latency: time.Since(t0), HTTPAttempted: 1, HTTPSucceeded: 1, CrossTenantChk: true, CrossTenantLk: true, IntegrityErr: true, AppFailure: true, Err: fmt.Errorf("RLS LEAK in list: caller=%s got=%s", u.TenantID, it.TenantID)}
		}
	}
	return OpOutcome{Kind: OpCRUDRead, StatusCode: st, Latency: time.Since(t0), HTTPAttempted: 1, HTTPSucceeded: 1, CrossTenantChk: true}
}

func (r *Runner) runCrossTenantNegative(ctx context.Context, caller SyntheticUser, foreignTenant, foreignID string) OpOutcome {
	t0 := time.Now()
	httpAtt := 0
	httpOK := 0
	if foreignID != "" {
		httpAtt++
		st, _, err := DoJSON(ctx, r.client, http.MethodGet, r.baseURL+"/api/collections/"+SoakItemsCollection+"/records/"+foreignID, caller.Token, nil)
		if err != nil {
			return classifyNetErr(OpCrossTenantDeny, time.Since(t0), httpAtt, httpOK, err)
		}
		if st != http.StatusNotFound && st != http.StatusForbidden {
			return OpOutcome{Kind: OpCrossTenantDeny, StatusCode: st, Latency: time.Since(t0), HTTPAttempted: httpAtt, HTTPSucceeded: httpAtt, CrossTenantChk: true, CrossTenantLk: true, IntegrityErr: true, AppFailure: true, Err: fmt.Errorf("CROSS-TENANT GET LEAK: %s read %s/%s status=%d", caller.TenantID, foreignTenant, foreignID, st)}
		}
		httpOK++
	}
	httpAtt++
	st, _, err := DoJSON(ctx, r.client, http.MethodPost, r.baseURL+"/api/collections/"+SoakItemsCollection+"/records", caller.Token, map[string]any{
		"tenant_id": foreignTenant,
		"title":     "FORGED_CROSS_TENANT_SOAK",
	})
	if err != nil {
		return classifyNetErr(OpCrossTenantDeny, time.Since(t0), httpAtt, httpOK, err)
	}
	if st == http.StatusOK || st == http.StatusCreated {
		return OpOutcome{Kind: OpCrossTenantDeny, StatusCode: st, Latency: time.Since(t0), HTTPAttempted: httpAtt, HTTPSucceeded: httpAtt, CrossTenantChk: true, CrossTenantLk: true, IntegrityErr: true, AppFailure: true, Err: fmt.Errorf("CROSS-TENANT WRITE LEAK: %s wrote into %s status=%d", caller.TenantID, foreignTenant, st)}
	}
	httpOK++
	return OpOutcome{Kind: OpCrossTenantDeny, StatusCode: st, Latency: time.Since(t0), HTTPAttempted: httpAtt, HTTPSucceeded: httpOK, CrossTenantChk: true}
}

func (r *Runner) runFileStorageJourney(ctx context.Context, u SyntheticUser) OpOutcome {
	t0 := time.Now()
	seq := r.seq.Add(1)
	payload := bytes.Repeat([]byte(fmt.Sprintf("ruxbase-soak-blob-%s-%08d-", u.TenantID, seq)), 1024)
	wantHash := sha256HexBytes(payload)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("tenant_id", u.TenantID)
	_ = mw.WriteField("owner", u.UserID)
	_ = mw.WriteField("category", "cat_file")
	_ = mw.WriteField("seq", fmt.Sprintf("%d", seq))
	_ = mw.WriteField("title", fmt.Sprintf("File %d", seq))
	_ = mw.WriteField("checksum", wantHash)
	part, err := mw.CreateFormFile("attachment", fmt.Sprintf("soak_%d.bin", seq))
	if err != nil {
		return OpOutcome{Kind: OpFileUpload, GenFailure: true, Latency: time.Since(t0), Err: err}
	}
	_, _ = part.Write(payload)
	_ = mw.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.baseURL+"/api/collections/"+SoakItemsCollection+"/records", &buf)
	if err != nil {
		return OpOutcome{Kind: OpFileUpload, GenFailure: true, Latency: time.Since(t0), Err: err}
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", u.Token)
	resp, err := r.client.Do(req)
	if err != nil {
		out := classifyNetErr(OpFileUpload, time.Since(t0), 1, 0, err)
		out.MutationsAttempted = 1
		return out
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return OpOutcome{Kind: OpFileUpload, StatusCode: resp.StatusCode, Latency: time.Since(t0), HTTPAttempted: 1, MutationsAttempted: 1, AppFailure: true, Err: fmt.Errorf("file upload status %d", resp.StatusCode)}
	}
	var created struct {
		ID         string `json:"id"`
		Attachment string `json:"attachment"`
	}
	if json.Unmarshal(raw, &created) != nil || created.ID == "" || created.Attachment == "" {
		return OpOutcome{Kind: OpFileUpload, StatusCode: resp.StatusCode, Latency: time.Since(t0), HTTPAttempted: 1, MutationsAttempted: 1, IntegrityErr: true, AppFailure: true, Err: errors.New("missing attachment filename")}
	}

	dlReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/api/files/%s/%s/%s", r.baseURL, SoakItemsCollection, created.ID, created.Attachment), nil)
	dlReq.Header.Set("Authorization", u.Token)
	dlResp, err := r.client.Do(dlReq)
	if err != nil {
		out := classifyNetErr(OpFileUpload, time.Since(t0), 2, 1, err)
		out.MutationsAttempted = 1
		out.MutationsCommitted = 1
		return out
	}
	h := sha256.New()
	_, copyErr := io.Copy(h, dlResp.Body)
	_ = dlResp.Body.Close()
	if copyErr != nil || dlResp.StatusCode != http.StatusOK {
		return OpOutcome{Kind: OpFileUpload, StatusCode: dlResp.StatusCode, Latency: time.Since(t0), HTTPAttempted: 2, HTTPSucceeded: 1, MutationsAttempted: 1, MutationsCommitted: 1, AppFailure: true, Err: fmt.Errorf("file download status=%d err=%v", dlResp.StatusCode, copyErr)}
	}
	gotHash := hex.EncodeToString(h.Sum(nil))
	if gotHash != wantHash {
		return OpOutcome{Kind: OpFileUpload, StatusCode: dlResp.StatusCode, Latency: time.Since(t0), HTTPAttempted: 2, HTTPSucceeded: 2, MutationsAttempted: 1, MutationsCommitted: 1, IntegrityErr: true, AppFailure: true, Err: fmt.Errorf("STORAGE SHA-256 MISMATCH: want=%s got=%s", wantHash, gotHash)}
	}

	delStatus, _, delErr := DoJSON(ctx, r.client, http.MethodDelete, r.baseURL+"/api/collections/"+SoakItemsCollection+"/records/"+created.ID, u.Token, nil)
	if delErr != nil || delStatus != http.StatusNoContent {
		return OpOutcome{Kind: OpFileUpload, StatusCode: delStatus, Latency: time.Since(t0), HTTPAttempted: 3, HTTPSucceeded: 2, MutationsAttempted: 2, MutationsCommitted: 1, AppFailure: true, Err: fmt.Errorf("file cleanup delete status=%d err=%v", delStatus, delErr)}
	}
	return OpOutcome{Kind: OpFileUpload, StatusCode: dlResp.StatusCode, Latency: time.Since(t0), HTTPAttempted: 3, HTTPSucceeded: 3, MutationsAttempted: 2, MutationsCommitted: 2}
}

func (r *Runner) runVectorSearch(ctx context.Context, u SyntheticUser, rng *rand.Rand) OpOutcome {
	t0 := time.Now()
	st, raw, err := DoJSON(ctx, r.client, http.MethodPost, r.baseURL+"/api/collections/"+SoakVectorsCollection+"/vector-search", u.Token, map[string]any{
		"field":  "embedding",
		"vector": MakeVector(rng.IntN(64)+1, r.fixtures.VectorDims),
		"k":      5,
	})
	if err != nil {
		return classifyNetErr(OpVectorSearch, time.Since(t0), 1, 0, err)
	}
	if st != http.StatusOK {
		return OpOutcome{Kind: OpVectorSearch, StatusCode: st, Latency: time.Since(t0), HTTPAttempted: 1, AppFailure: true, Err: fmt.Errorf("vector-search status %d", st)}
	}
	var vr struct {
		Items []struct {
			TenantID string `json:"tenant_id"`
		} `json:"items"`
	}
	if json.Unmarshal(raw, &vr) == nil {
		for _, it := range vr.Items {
			if it.TenantID != u.TenantID {
				return OpOutcome{Kind: OpVectorSearch, StatusCode: st, Latency: time.Since(t0), HTTPAttempted: 1, HTTPSucceeded: 1, CrossTenantChk: true, CrossTenantLk: true, IntegrityErr: true, AppFailure: true, Err: fmt.Errorf("VECTOR CROSS-TENANT LEAK: caller=%s got=%s", u.TenantID, it.TenantID)}
			}
		}
	}
	return OpOutcome{Kind: OpVectorSearch, StatusCode: st, Latency: time.Since(t0), HTTPAttempted: 1, HTTPSucceeded: 1, CrossTenantChk: true}
}

func (r *Runner) runRealtimeSync(ctx context.Context, u SyntheticUser) OpOutcome {
	t0 := time.Now()
	since := time.Now().UTC().Add(-2 * time.Minute).Format(time.RFC3339)
	target := fmt.Sprintf("%s/api/realtime/sync?collection=%s&since=%s", r.baseURL, url.QueryEscape(SoakItemsCollection), url.QueryEscape(since))
	st, _, err := DoJSON(ctx, r.client, http.MethodGet, target, u.Token, nil)
	if err != nil {
		return classifyNetErr(OpRealtimeSync, time.Since(t0), 1, 0, err)
	}
	if st != http.StatusOK {
		return OpOutcome{Kind: OpRealtimeSync, StatusCode: st, Latency: time.Since(t0), HTTPAttempted: 1, AppFailure: true, Err: fmt.Errorf("realtime sync status %d", st)}
	}
	return OpOutcome{Kind: OpRealtimeSync, StatusCode: st, Latency: time.Since(t0), HTTPAttempted: 1, HTTPSucceeded: 1}
}

// RunExplainProbe executes periodic EXPLAIN (ANALYZE, BUFFERS) on safe representative queries.
func RunExplainProbe(ctx context.Context, pool *pgxpool.Pool, tenantID string) ([]string, error) {
	if pool == nil {
		return nil, nil
	}
	rows, err := pool.Query(ctx, `
		EXPLAIN (ANALYZE, BUFFERS)
		SELECT id, tenant_id, category, seq, title
		FROM soak_items
		WHERE tenant_id = $1
		ORDER BY seq DESC
		LIMIT 20
	`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var line string
		if rows.Scan(&line) == nil {
			plan = append(plan, line)
		}
	}
	return plan, rows.Err()
}

// SSEPool maintains persistent multi-tenant SSE subscriptions with periodic reconnects
// and cross-tenant event isolation checks (Journey D).
type SSEPool struct {
	baseURL  string
	fixtures *Fixtures
	client   *http.Client
	count    int

	ActiveClients atomic.Int64
	EventsRecv    atomic.Int64
	Reconnects    atomic.Int64
	Leaks         atomic.Int64
}

// NewSSEPool creates a persistent SSE subscriber manager.
func NewSSEPool(baseURL string, fixtures *Fixtures, count int) *SSEPool {
	if count <= 0 {
		count = 8
	}
	return &SSEPool{
		baseURL:  strings.TrimRight(baseURL, "/"),
		fixtures: fixtures,
		client:   NewHTTPClient(15 * time.Second),
		count:    count,
	}
}

// Run launches persistent SSE subscribers until ctx is canceled.
func (p *SSEPool) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for i := range p.count {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			p.runSubscriber(ctx, idx)
		}(i)
	}
	wg.Wait()
}

func (p *SSEPool) runSubscriber(ctx context.Context, idx int) {
	lastEventID := ""
	streamClient := &http.Client{Transport: p.client.Transport}
	for ctx.Err() == nil {
		p.fixtures.mu.RLock()
		if len(p.fixtures.Users) == 0 {
			p.fixtures.mu.RUnlock()
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
				continue
			}
		}
		user := p.fixtures.Users[idx%len(p.fixtures.Users)]
		p.fixtures.mu.RUnlock()

		connTTL := time.Duration(15+idx%10) * time.Minute
		connCtx, connCancel := context.WithTimeout(ctx, connTTL)
		req, err := http.NewRequestWithContext(connCtx, http.MethodGet, p.baseURL+"/api/realtime", nil)
		if err != nil {
			connCancel()
			return
		}
		if lastEventID != "" {
			req.Header.Set("Last-Event-ID", lastEventID)
		}
		resp, err := streamClient.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			if resp != nil {
				_ = resp.Body.Close()
			}
			connCancel()
			p.Reconnects.Add(1)
			select {
			case <-ctx.Done():
				return
			case <-time.After(3 * time.Second):
				continue
			}
		}

		p.ActiveClients.Add(1)
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 32*1024), 512*1024)
		subscribed := false
		var curEvent, curID string
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "id:") {
				curID = strings.TrimSpace(strings.TrimPrefix(line, "id:"))
				continue
			}
			if strings.HasPrefix(line, "event:") {
				curEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
				continue
			}
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if curEvent == "PB_CONNECT" && !subscribed {
				var d struct {
					ClientId string `json:"clientId"`
				}
				if json.Unmarshal([]byte(payload), &d) == nil && d.ClientId != "" {
					_, _, _ = DoJSON(connCtx, p.client, http.MethodPost, p.baseURL+"/api/realtime", user.Token, map[string]any{
						"clientId":      d.ClientId,
						"subscriptions": []string{SoakItemsCollection},
					})
					subscribed = true
				}
				continue
			}
			if curEvent == "PB_RESYNC_REQUIRED" {
				continue
			}
			var evt struct {
				Record struct {
					TenantID string `json:"tenant_id"`
				} `json:"record"`
			}
			if json.Unmarshal([]byte(payload), &evt) == nil {
				p.EventsRecv.Add(1)
				if curID != "" {
					lastEventID = curID
				}
				if evt.Record.TenantID != "" && evt.Record.TenantID != user.TenantID {
					p.Leaks.Add(1)
				}
			}
		}
		_ = resp.Body.Close()
		p.ActiveClients.Add(-1)
		connCancel()
		if ctx.Err() == nil {
			p.Reconnects.Add(1)
			select {
			case <-ctx.Done():
				return
			case <-time.After(1 * time.Second):
			}
		}
	}
}

func (r *Runner) returnID(tenantID, id string) {
	if id == "" {
		return
	}
	r.recentMu.Lock()
	defer r.recentMu.Unlock()
	list := r.recentIDs[tenantID]
	if len(list) >= 200 {
		list = list[1:]
	}
	r.recentIDs[tenantID] = append(list, id)
}

// checkoutID removes one ID exclusively from the pool so two goroutines never race
// concurrent updates/deletes on the same record.
func (r *Runner) checkoutID(tenantID string) string {
	r.recentMu.Lock()
	defer r.recentMu.Unlock()
	list := r.recentIDs[tenantID]
	if len(list) == 0 {
		return ""
	}
	id := list[len(list)-1]
	r.recentIDs[tenantID] = list[:len(list)-1]
	return id
}

func classifyNetErr(kind OpKind, d time.Duration, httpAtt, httpOK int, err error) OpOutcome {
	timeout := errors.Is(err, context.DeadlineExceeded)
	if !timeout {
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			timeout = true
		}
	}
	if errors.Is(err, context.Canceled) {
		return OpOutcome{Kind: kind, Latency: d, HTTPAttempted: httpAtt, HTTPSucceeded: httpOK, GenFailure: true, Err: err}
	}
	return OpOutcome{Kind: kind, Latency: d, HTTPAttempted: httpAtt, HTTPSucceeded: httpOK, AppFailure: true, Timeout: timeout, Err: err}
}

// PercentileMs computes the p-th percentile in milliseconds from a slice of durations.
func PercentileMs(samples []time.Duration, p float64) float64 {
	if len(samples) == 0 {
		return 0
	}
	cp := make([]time.Duration, len(samples))
	copy(cp, samples)
	slices.Sort(cp)
	idx := int(math.Ceil(float64(len(cp))*p/100.0)) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(cp) {
		idx = len(cp) - 1
	}
	return math.Round(float64(cp[idx].Microseconds())/1000.0*1000) / 1000
}

// MakeVector generates a deterministic unit-like vector of length dims.
func MakeVector(seed, dims int) []float64 {
	if dims <= 0 {
		dims = 128
	}
	v := make([]float64, dims)
	for d := range dims {
		v[d] = math.Round(math.Sin(float64(seed)*0.017+float64(d+1)*0.19)*100000) / 100000
	}
	return v
}

func sha256Hex(s string) string {
	return sha256HexBytes([]byte(s))
}

func sha256HexBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
