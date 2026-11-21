package core

import (
	"fmt"
	"strings"
)

// ruxbase: Unified authorization helpers and cross-path policy contracts.

const (
	AuthzOpList   = "list"
	AuthzOpView   = "view"
	AuthzOpCreate = "create"
	AuthzOpUpdate = "update"
	AuthzOpDelete = "delete"
)

// CollectionRuleForOp returns the rule pointer associated with the specified operation.
func CollectionRuleForOp(collection *Collection, op string) (*string, error) {
	if collection == nil {
		return nil, fmt.Errorf("collection cannot be nil")
	}

	switch strings.ToLower(op) {
	case AuthzOpList:
		return collection.ListRule, nil
	case AuthzOpView:
		return collection.ViewRule, nil
	case AuthzOpCreate:
		return collection.CreateRule, nil
	case AuthzOpUpdate:
		return collection.UpdateRule, nil
	case AuthzOpDelete:
		return collection.DeleteRule, nil
	default:
		return nil, fmt.Errorf("unsupported authorization operation %q", op)
	}
}

// CanAccessCollectionOp determines if an operation is authorized at the collection level.
//
// Returns:
//   - allowed: whether access is permitted at the collection level
//   - rule: the specific rule pointer if conditional record evaluation is required
//   - err: any error encountered
//
// Invariants:
//   - Superusers are always allowed without rule restriction.
//   - If the rule is nil, access is denied (admin only).
//   - If the rule is empty (""), access is publicly allowed.
//   - If the rule has an expression, allowed is true but rule is returned for row-level evaluation.
func CanAccessCollectionOp(collection *Collection, requestInfo *RequestInfo, op string) (bool, *string, error) {
	if collection == nil {
		return false, nil, fmt.Errorf("collection cannot be nil")
	}

	if requestInfo == nil {
		requestInfo = &RequestInfo{}
	}

	if requestInfo.HasSuperuserAuth() {
		return true, nil, nil
	}

	rule, err := CollectionRuleForOp(collection, op)
	if err != nil {
		return false, nil, err
	}

	if rule == nil {
		return false, nil, nil
	}

	return true, rule, nil
}

// IsRuleSupportedInRLS checks if an access rule can be translated to a PostgreSQL RLS policy.
// If unsupported, it returns false along with the reason (such as reliance on HTTP request-only context).
func IsRuleSupportedInRLS(rule string) (bool, string) {
	trimmed := strings.TrimSpace(rule)
	if trimmed == "" {
		return true, ""
	}

	unsupportedPatterns := []struct {
		pattern string
		reason  string
	}{
		{"@request.headers.", "request headers are available only within HTTP API context"},
		{"@request.body.", "request body is available only within HTTP API mutation context"},
		{"@request.query.", "request query parameters are available only within HTTP API context"},
		{"@request.method", "HTTP method is available only within HTTP API context"},
		{"@request.context", "request context is available only within HTTP API context"},
		{":isset", ":isset modifier is not translatable to SQL RLS"},
		{":changed", ":changed modifier is not translatable to SQL RLS"},
	}

	for _, up := range unsupportedPatterns {
		if strings.Contains(trimmed, up.pattern) {
			return false, up.reason
		}
	}

	return true, ""
}
