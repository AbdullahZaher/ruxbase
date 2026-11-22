package core

import (
	"errors"
	"time"

	"github.com/pocketbase/dbx"
)

// rlsStrictTokenDuration is the duration of the RLS tokens generated for the strict mode queries.
const rlsStrictTokenDuration = time.Minute

// IsRLSStrict reports whether the RLS strict mode is enabled
// (the API record queries of the non-superusers are executed as the RLS API role).
func (app *BaseApp) IsRLSStrict() bool {
	return app.config.RLSStrict
}

// RunAsRLSRole executes fn in a new transaction as the RLS API role
// (RLSAPIRole) with the provided auth record (nil for the guests), so that
// the RLS policies apply to the queries of the transaction app.
//
// The optional rule (RLSRuleList or RLSRuleView) restricts the SELECT policies
// to the policy of that collection rule.
func RunAsRLSRole(app App, auth *Record, rule string, fn func(txApp App) error) error {
	if auth != nil && auth.IsSuperuser() {
		return errors.New("the superusers bypass the RLS policies")
	}

	return app.RunInTransaction(func(txApp App) error {
		var setClaimsSQL string
		params := dbx.Params{
			"role":        RLSAPIRole,
			"ruleSetting": RLSRuleSetting,
			"rule":        rule,
		}

		if auth != nil {
			expEpoch := time.Now().Add(rlsStrictTokenDuration).Unix()
			setClaimsSQL = "SELECT " + RLSSchema + ".set_claims_direct({:authId}, {:collectionId}, {:collectionName}, {:expEpoch}, true), set_config('role', {:role}, true), set_config({:ruleSetting}, {:rule}, true)"
			params["authId"] = auth.Id
			params["collectionId"] = auth.Collection().Id
			params["collectionName"] = auth.Collection().Name
			params["expEpoch"] = expEpoch
		} else {
			setClaimsSQL = "SELECT set_config('role', {:role}, true), set_config({:tokenSetting}, '', true), set_config({:ruleSetting}, {:rule}, true)"
			params["tokenSetting"] = RLSTokenSetting
		}

		_, err := txApp.DB().NewQuery(setClaimsSQL).Bind(params).Execute()
		if err != nil {
			return err
		}

		return fn(txApp)
	})
}
