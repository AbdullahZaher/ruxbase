package migrations

import (
	"github.com/AbdullahZaher/ruxbase/core"
)

// ruxbase v1.1 RLS changes.
//
// Reinstalls the RLS functions and policies (if RLS is installed): ruxbase_rls.auth()
// accepts only the RLS tokens and the view rule policy applies only to the strict
// mode API role.
//
// Also fixes the search_path of the ruxbase_strftime SQL functions, because they
// call the other ruxbase_strftime_* functions and they could be inlined in the
// queries (and policies) of other roles.
func init() {
	core.SystemMigrations.Register(func(txApp core.App) error {
		_, err := txApp.DB().NewQuery(`
			DO $ruxbase$
			DECLARE
				f regprocedure;
			BEGIN
				FOR f IN
					SELECT p.oid::regprocedure FROM pg_proc p
					WHERE p.pronamespace = current_schema()::regnamespace AND p.proname = 'ruxbase_strftime'
				LOOP
					EXECUTE format('ALTER FUNCTION %s SET search_path = pg_catalog, %I, pg_temp', f, current_schema());
				END LOOP;
			END
			$ruxbase$;
		`).Execute()
		if err != nil {
			return err
		}

		var installed bool
		err = txApp.DB().NewQuery(`
			SELECT EXISTS (
				SELECT 1 FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
				WHERE n.nspname = 'ruxbase_rls' AND p.proname = 'auth'
			)
		`).Row(&installed)
		if err != nil || !installed {
			return err
		}

		return core.InstallRLS(txApp)
	}, nil)
}
