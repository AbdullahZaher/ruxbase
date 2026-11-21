package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
)

// ruxbase: Transaction-local principal/tenant scoping and safe role switching.

const (
	// RLSTenantSetting is the PostgreSQL setting holding the current transaction's tenant identifier.
	RLSTenantSetting = "ruxbase.tenant_id"

	// defaultScopedTokenDuration is the default validity duration for scoped RLS tokens.
	defaultScopedTokenDuration = time.Minute
)

// TransactionScope defines the principal, role, tenant, and claim parameters
// for executing work inside an isolated, transaction-scoped boundary.
type TransactionScope struct {
	// Principal is an optional authenticated auth record.
	Principal *Record

	// Role specifies the PostgreSQL role to switch to for the duration of the transaction.
	// If empty and Principal is provided, defaults to RLSAuthenticatedRole.
	// If empty and Principal is nil, defaults to RLSAnonRole.
	Role string

	// Rule optionally sets the RLSRuleSetting (e.g. RLSRuleList or RLSRuleView).
	Rule string

	// TenantID is an optional tenant identifier bound locally to ruxbase.tenant_id for the transaction.
	TenantID string

	// TokenDuration specifies the duration of the generated RLS token (defaults to 1 minute).
	TokenDuration time.Duration

	// Claims specifies additional transaction-local configuration settings.
	Claims map[string]string

	// ReadOnly specifies whether the transaction should execute with READ ONLY semantics.
	ReadOnly bool
}

// RunInScopedTransaction executes fn inside a transaction configured with the provided TransactionScope.
func RunInScopedTransaction(app App, scope TransactionScope, fn func(txApp App) error) error {
	return RunInScopedTransactionWithContext(context.Background(), app, scope, fn)
}

// RunInScopedTransaction wraps fn into a transaction configured with the provided
// TransactionScope for the regular app database.
func (app *BaseApp) RunInScopedTransaction(scope TransactionScope, fn func(txApp App) error) error {
	return RunInScopedTransactionWithContext(context.Background(), app, scope, fn)
}

// RunInScopedTransactionWithContext wraps fn into a context-aware transaction configured
// with the provided TransactionScope for the regular app database.
func (app *BaseApp) RunInScopedTransactionWithContext(ctx context.Context, scope TransactionScope, fn func(txApp App) error) error {
	return RunInScopedTransactionWithContext(ctx, app, scope, fn)
}

// RunInScopedTransactionWithContext executes fn inside a transaction configured with the provided
// TransactionScope and context, ensuring that role switches and claims are strictly transaction-local.
func RunInScopedTransactionWithContext(ctx context.Context, app App, scope TransactionScope, fn func(txApp App) error) error {
	if app == nil {
		return errors.New("app cannot be nil")
	}

	// 1. Resolve role
	role := strings.TrimSpace(scope.Role)
	if role == "" && scope.Principal != nil {
		role = RLSAuthenticatedRole
	}

	// 2. Reject unsafe or elevated role escalation attempts
	if role != "" {
		lowerRole := strings.ToLower(role)
		if lowerRole == "postgres" || lowerRole == "admin" || lowerRole == "superuser" || strings.Contains(lowerRole, "superuser") {
			return fmt.Errorf("unsafe role escalation to %q is forbidden in scoped transaction", role)
		}
	}

	// 3. Resolve and generate RLS token
	var token string
	if scope.Principal != nil {
		if scope.Principal.IsSuperuser() {
			return errors.New("superusers bypass collection RLS policies and cannot execute within restricted RLS scope")
		}

		duration := scope.TokenDuration
		if duration <= 0 {
			duration = defaultScopedTokenDuration
		}

		var err error
		token, err = scope.Principal.NewRLSToken(duration)
		if err != nil {
			return fmt.Errorf("failed to generate scoped RLS token: %w", err)
		}
	}

	// 4. Execute within context-aware transaction
	return app.RunInTransactionWithContext(ctx, func(txApp App) error {
		// Set transaction read-only if requested
		if scope.ReadOnly {
			if _, err := txApp.DB().NewQuery("SET TRANSACTION READ ONLY").WithContext(ctx).Execute(); err != nil {
				return fmt.Errorf("failed to set transaction read only: %w", err)
			}
		}

		// Configure transaction-local token, rule, tenant settings, and optional role (is_local = true)
		params := dbx.Params{
			"tokenSetting":  RLSTokenSetting,
			"token":         token,
			"ruleSetting":   RLSRuleSetting,
			"rule":          scope.Rule,
			"tenantSetting": RLSTenantSetting,
			"tenant":        scope.TenantID,
		}

		configParts := []string{
			"set_config({:tokenSetting}, {:token}, true)",
			"set_config({:ruleSetting}, {:rule}, true)",
			"set_config({:tenantSetting}, {:tenant}, true)",
		}

		if role != "" {
			params["role"] = role
			configParts = append([]string{"set_config('role', {:role}, true)"}, configParts...)
		}

		configSQL := "SELECT " + strings.Join(configParts, ", ")

		if _, err := txApp.DB().NewQuery(configSQL).Bind(params).WithContext(ctx).Execute(); err != nil {
			return fmt.Errorf("failed to set scoped transaction parameters: %w", err)
		}

		// Apply additional user-specified transaction-local claims
		for k, v := range scope.Claims {
			claimSettingParam := "claim_key_" + strings.ReplaceAll(k, ".", "_")
			claimValParam := "claim_val_" + strings.ReplaceAll(k, ".", "_")
			claimSQL := fmt.Sprintf("SELECT set_config({:%s}, {:%s}, true)", claimSettingParam, claimValParam)
			if _, err := txApp.DB().NewQuery(claimSQL).Bind(dbx.Params{
				claimSettingParam: k,
				claimValParam:     v,
			}).WithContext(ctx).Execute(); err != nil {
				return fmt.Errorf("failed to set transaction claim %q: %w", k, err)
			}
		}

		return fn(txApp)
	})
}
