package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"strings"
)

const (
	stressItemsCol   = "stress_items"
	stressPublicCol  = "stress_public"
	stressVectorsCol = "stress_vectors"
	stressNumTenants = 20
	stressUserPass   = "StressUserPass!2026"
)

type TenantUser struct {
	Index    int    `json:"index"`
	TenantID string `json:"tenantId"`
	UserID   string `json:"userId"`
	Email    string `json:"email"`
	Password string `json:"password"`
	Token    string `json:"token"`
}

type FixtureState struct {
	SuperuserEmail string       `json:"superuserEmail"`
	SuperuserToken string       `json:"superuserToken"`
	Tenants        []TenantUser `json:"tenants"`
	ItemIDsByTen   [][]string   `json:"itemIdsByTenant"`
	PublicIDs      []string     `json:"publicIds"`
	InitialItems   int          `json:"initialItems"`
	VectorDims     int          `json:"vectorDims"`
	InitialVectors int          `json:"initialVectors"`
	RLSApplied     bool         `json:"rlsApplied"`
}

func saveFixtureState(path string, st *FixtureState) error {
	if path == "" {
		return nil
	}
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

func loadFixtureState(path string) (*FixtureState, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var st FixtureState
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func authSuperuser(client *http.Client, baseURL, email, password string) (string, error) {
	status, raw, err := doJSON(client, http.MethodPost, baseURL+"/api/collections/_superusers/auth-with-password", "", map[string]string{
		"identity": email,
		"password": password,
	})
	if err != nil || status != http.StatusOK {
		return "", fmt.Errorf("superuser auth failed: status=%d err=%v body=%s", status, err, string(raw))
	}
	var resp struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil || resp.Token == "" {
		return "", fmt.Errorf("missing superuser token")
	}
	return resp.Token, nil
}

func setupStressFixtures(ctx context.Context, client *http.Client, pg *pgTelemetry, baseURL, email, password string, initialItems, initialVectors int, stateFile string) (*FixtureState, error) {
	token, err := authSuperuser(client, baseURL, email, password)
	if err != nil {
		return nil, err
	}

	// Verify restricted role if PG admin telemetry is connected
	if pg != nil {
		if err := pg.verifyAppRoleRestricted(ctx, "ruxbase_app"); err != nil {
			return nil, err
		}
	}

	// 1. Add tenant_id field to `users` collection if absent
	status, raw, err := doJSON(client, http.MethodGet, baseURL+"/api/collections/users", token, nil)
	if err != nil || status != http.StatusOK {
		return nil, fmt.Errorf("get users collection: status=%d err=%v body=%s", status, err, string(raw))
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
		status, raw, err = doJSON(client, http.MethodPatch, baseURL+"/api/collections/users", token, map[string]any{
			"fields": fields,
		})
		if err != nil || status != http.StatusOK {
			return nil, fmt.Errorf("patch users collection with tenant_id: status=%d err=%v body=%s", status, err, string(raw))
		}
	}

	// 2. Recreate clean stress collections
	for _, col := range []string{stressItemsCol, stressPublicCol, stressVectorsCol} {
		_, _, _ = doJSON(client, http.MethodDelete, baseURL+"/api/collections/"+col, token, nil)
	}

	// Multi-tenant RLS collection
	rlsRule := "tenant_id = @request.auth.tenant_id"
	createRule := `@request.auth.id != "" && tenant_id = @request.auth.tenant_id`
	status, raw, err = doJSON(client, http.MethodPost, baseURL+"/api/collections", token, map[string]any{
		"name":       stressItemsCol,
		"type":       "base",
		"listRule":   rlsRule,
		"viewRule":   rlsRule,
		"createRule": createRule,
		"updateRule": rlsRule,
		"deleteRule": rlsRule,
		"fields": []map[string]any{
			{"name": "tenant_id", "type": "text", "required": true},
			{"name": "owner", "type": "text"},
			{"name": "category", "type": "text"},
			{"name": "n", "type": "number"},
			{"name": "amount", "type": "number"},
			{"name": "published", "type": "bool"},
			{"name": "title", "type": "text"},
			{"name": "body", "type": "text"},
			{"name": "unindexed_tag", "type": "text"},
			{"name": "sent", "type": "number"},
			{"name": "attachment", "type": "file", "maxSelect": 1, "maxSize": 20 * 1024 * 1024},
			{"name": "created", "type": "autodate", "onCreate": true},
			{"name": "updated", "type": "autodate", "onCreate": true, "onUpdate": true},
		},
		"indexes": []string{
			"CREATE INDEX idx_stress_items_tenant_n ON stress_items (tenant_id, n DESC)",
			"CREATE INDEX idx_stress_items_tenant_cat ON stress_items (tenant_id, category)",
			"CREATE INDEX idx_stress_items_owner ON stress_items (owner)",
		},
	})
	if err != nil || status != http.StatusOK {
		return nil, fmt.Errorf("create %s: status=%d err=%v body=%s", stressItemsCol, status, err, string(raw))
	}

	// Verify RLS compiled and applied on stress_items
	rlsStatus, rlsRaw, err := doJSON(client, http.MethodGet, baseURL+"/api/collections/"+stressItemsCol+"/rls", token, nil)
	rlsApplied := false
	if err == nil && rlsStatus == http.StatusOK {
		var rlsResp struct {
			Applied     bool   `json:"applied"`
			ApplyStatus string `json:"applyStatus"`
		}
		if json.Unmarshal(rlsRaw, &rlsResp) == nil {
			rlsApplied = rlsResp.Applied || rlsResp.ApplyStatus == "applied"
		}
	}
	log.Printf("setup: collection %s created (rlsApplied=%v)", stressItemsCol, rlsApplied)

	// Public collection
	status, raw, err = doJSON(client, http.MethodPost, baseURL+"/api/collections", token, map[string]any{
		"name":       stressPublicCol,
		"type":       "base",
		"listRule":   "",
		"viewRule":   "",
		"createRule": `@request.auth.id != ""`,
		"fields": []map[string]any{
			{"name": "title", "type": "text"},
			{"name": "body", "type": "text"},
			{"name": "n", "type": "number"},
			{"name": "published", "type": "bool"},
			{"name": "sent", "type": "number"},
		},
		"indexes": []string{
			"CREATE INDEX idx_stress_public_n ON stress_public (n DESC)",
		},
	})
	if err != nil || status != http.StatusOK {
		return nil, fmt.Errorf("create %s: status=%d err=%v body=%s", stressPublicCol, status, err, string(raw))
	}

	// Vector collection (384-dim cosine HNSW, tenant-isolated)
	const vecDims = 384
	status, raw, err = doJSON(client, http.MethodPost, baseURL+"/api/collections", token, map[string]any{
		"name":       stressVectorsCol,
		"type":       "base",
		"listRule":   rlsRule,
		"viewRule":   rlsRule,
		"createRule": createRule,
		"updateRule": rlsRule,
		"deleteRule": rlsRule,
		"fields": []map[string]any{
			{"name": "tenant_id", "type": "text", "required": true},
			{"name": "label", "type": "text"},
			{"name": "n", "type": "number"},
			{"name": "embedding", "type": "vector", "dimensions": vecDims, "metric": "cosine", "index": true},
		},
		"indexes": []string{
			"CREATE INDEX idx_stress_vectors_tenant ON stress_vectors (tenant_id)",
		},
	})
	if err != nil || status != http.StatusOK {
		return nil, fmt.Errorf("create %s: status=%d err=%v body=%s", stressVectorsCol, status, err, string(raw))
	}

	// 3. Create or authenticate 20 tenant users
	tenants := make([]TenantUser, stressNumTenants)
	for i := range stressNumTenants {
		tid := fmt.Sprintf("tenant_%02d", i)
		uEmail := fmt.Sprintf("user_%02d@stress.local", i)
		// Try create; if already exists, auth directly
		st, body, _ := doJSON(client, http.MethodPost, baseURL+"/api/collections/users/records", token, map[string]any{
			"email":           uEmail,
			"password":        stressUserPass,
			"passwordConfirm": stressUserPass,
			"verified":        true,
			"tenant_id":       tid,
		})
		if st != http.StatusOK && !strings.Contains(string(body), "validation_not_unique") {
			return nil, fmt.Errorf("create user %s: status=%d body=%s", uEmail, st, string(body))
		}
		st, body, err = doJSON(client, http.MethodPost, baseURL+"/api/collections/users/auth-with-password", "", map[string]string{
			"identity": uEmail,
			"password": stressUserPass,
		})
		if err != nil || st != http.StatusOK {
			return nil, fmt.Errorf("auth user %s: status=%d err=%v body=%s", uEmail, st, err, string(body))
		}
		var authResp struct {
			Token  string `json:"token"`
			Record struct {
				ID       string `json:"id"`
				TenantID string `json:"tenant_id"`
			} `json:"record"`
		}
		if err := json.Unmarshal(body, &authResp); err != nil || authResp.Token == "" {
			return nil, fmt.Errorf("decode auth user %s: %v", uEmail, err)
		}
		if authResp.Record.TenantID != tid {
			_, _, _ = doJSON(client, http.MethodPatch, baseURL+"/api/collections/users/records/"+authResp.Record.ID, token, map[string]any{
				"tenant_id": tid,
			})
		}
		tenants[i] = TenantUser{
			Index:    i,
			TenantID: tid,
			UserID:   authResp.Record.ID,
			Email:    uEmail,
			Password: stressUserPass,
			Token:    authResp.Token,
		}
	}

	// 4. Fast-seed stress_items, stress_public, and stress_vectors via PG if available, else HTTP
	itemIDsByTen := make([][]string, stressNumTenants)
	publicIDs := make([]string, 0, 2000)

	if pg != nil {
		log.Printf("setup: fast-seeding %d stress_items, 2000 stress_public, %d stress_vectors via PostgreSQL", initialItems, initialVectors)
		_, err = pg.pool.Exec(ctx, `
			INSERT INTO stress_items (id, tenant_id, owner, category, n, amount, published, title, body, unindexed_tag, sent, attachment, created, updated)
			SELECT
				'si' || lpad(g::text, 13, '0'),
				'tenant_' || lpad((g % $2)::text, 2, '0'),
				'owner_' || lpad((g % $2)::text, 2, '0'),
				'cat_' || (g % 10)::text,
				g::double precision,
				((g % 1000) + 1.5)::double precision,
				(g % 2 = 0),
				'Stress Item #' || g::text,
				repeat('enterprise workload payload data ', 8),
				'tag_' || (g % 500)::text,
				0,
				'',
				now() - ((($1 - g) % 86400) || ' seconds')::interval,
				now()
			FROM generate_series(1, $1) AS g
		`, initialItems, stressNumTenants)
		if err != nil {
			return nil, fmt.Errorf("seed stress_items: %w", err)
		}

		_, err = pg.pool.Exec(ctx, `
			INSERT INTO stress_public (id, title, body, n, published, sent)
			SELECT
				'sp' || lpad(g::text, 13, '0'),
				'Public Item #' || g::text,
				repeat('public catalog description text ', 8),
				g::double precision,
				(g % 2 = 0),
				0
			FROM generate_series(1, 2000) AS g
		`)
		if err != nil {
			return nil, fmt.Errorf("seed stress_public: %w", err)
		}

		if initialVectors > 0 {
			_, err = pg.pool.Exec(ctx, `
				INSERT INTO stress_vectors (id, tenant_id, label, n, embedding)
				SELECT
					'sv' || lpad(g::text, 13, '0'),
					'tenant_' || lpad((g % 10)::text, 2, '0'),
					'cluster_' || (g % 50)::text,
					g::double precision,
					(
						SELECT ('[' || string_agg(round((sin((g * 0.013 + d * 0.17)::double precision))::numeric, 5)::text, ',') || ']')::vector
						FROM generate_series(1, $2) AS d
					)
				FROM generate_series(1, $1) AS g
			`, initialVectors, vecDims)
			if err != nil {
				return nil, fmt.Errorf("seed stress_vectors: %w", err)
			}
		}

		// Create SQL RPC function owned by ruxbase_app (SECURITY INVOKER)
		_, err = pg.pool.Exec(ctx, `
			CREATE OR REPLACE FUNCTION public.stress_tenant_summary(p_tenant text)
			RETURNS TABLE(item_count bigint, avg_n double precision, sum_amount double precision)
			LANGUAGE sql
			STABLE
			AS $$
				SELECT count(*), COALESCE(avg(n), 0), COALESCE(sum(amount), 0)
				FROM public.stress_items
				WHERE tenant_id = p_tenant;
			$$;
			ALTER FUNCTION public.stress_tenant_summary(text) OWNER TO ruxbase_app;
			GRANT EXECUTE ON FUNCTION public.stress_tenant_summary(text) TO ruxbase_app, ruxbase_authenticated, ruxbase_api;
			ANALYZE stress_items;
			ANALYZE stress_public;
			ANALYZE stress_vectors;
		`)
		if err != nil {
			return nil, fmt.Errorf("create SQL RPC function: %w", err)
		}

		// Expose SQL RPC function through Ruxbase admin API
		rule := `@request.auth.id != ""`
		st, rawBody, err := doJSON(client, http.MethodPost, baseURL+"/api/ruxbase/rpc/expose", token, map[string]any{
			"functionName": "stress_tenant_summary",
			"schema":       "public",
			"accessRule":   rule,
		})
		if err != nil || (st != http.StatusOK && st != http.StatusCreated) {
			log.Printf("warning: expose SQL RPC returned status=%d err=%v body=%s", st, err, string(rawBody))
		}

		for g := 1; g <= initialItems; g++ {
			tIdx := g % stressNumTenants
			if len(itemIDsByTen[tIdx]) < 500 {
				itemIDsByTen[tIdx] = append(itemIDsByTen[tIdx], fmt.Sprintf("si%013d", g))
			}
		}
		for g := 1; g <= 2000; g++ {
			publicIDs = append(publicIDs, fmt.Sprintf("sp%013d", g))
		}
	} else {
		// HTTP fallback seeding
		for g := 1; g <= initialItems; g++ {
			tIdx := g % stressNumTenants
			id := fmt.Sprintf("si%013d", g)
			st, rawBody, err := doJSON(client, http.MethodPost, baseURL+"/api/collections/"+stressItemsCol+"/records", tenants[tIdx].Token, map[string]any{
				"id":            id,
				"tenant_id":     tenants[tIdx].TenantID,
				"owner":         tenants[tIdx].UserID,
				"category":      fmt.Sprintf("cat_%d", g%10),
				"n":             g,
				"amount":        float64(g%1000) + 1.5,
				"published":     g%2 == 0,
				"title":         fmt.Sprintf("Stress Item #%d", g),
				"body":          strings.Repeat("enterprise workload payload data ", 8),
				"unindexed_tag": fmt.Sprintf("tag_%d", g%500),
			})
			if err != nil || st != http.StatusOK {
				return nil, fmt.Errorf("http seed item %d: status=%d err=%v body=%s", g, st, err, string(rawBody))
			}
			if len(itemIDsByTen[tIdx]) < 500 {
				itemIDsByTen[tIdx] = append(itemIDsByTen[tIdx], id)
			}
		}
	}

	// Cross-tenant smoke check before any load test runs
	t0 := tenants[0]
	t1ID := itemIDsByTen[1][0]
	st, _, err := doJSON(client, http.MethodGet, baseURL+"/api/collections/"+stressItemsCol+"/records/"+t1ID, t0.Token, nil)
	if err != nil || st != http.StatusNotFound {
		return nil, fmt.Errorf("pre-test cross-tenant check failed: tenant_00 reading tenant_01 record %s returned status=%d err=%v (expected 404)", t1ID, st, err)
	}
	log.Printf("setup: pre-test cross-tenant negative check PASSED (tenant_00 -> tenant_01 record => HTTP 404)")

	state := &FixtureState{
		SuperuserEmail: email,
		SuperuserToken: token,
		Tenants:        tenants,
		ItemIDsByTen:   itemIDsByTen,
		PublicIDs:      publicIDs,
		InitialItems:   initialItems,
		VectorDims:     vecDims,
		InitialVectors: initialVectors,
		RLSApplied:     rlsApplied,
	}
	if err := saveFixtureState(stateFile, state); err != nil {
		return nil, err
	}
	return state, nil
}

func makeQueryVector(seed int, dims int) []float64 {
	v := make([]float64, dims)
	for d := 1; d <= dims; d++ {
		v[d-1] = math.Round(math.Sin(float64(seed)*0.013+float64(d)*0.17)*100000) / 100000
	}
	return v
}
