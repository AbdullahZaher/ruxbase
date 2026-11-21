package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/AbdullahZaher/ruxbase/tools/security"
	"github.com/AbdullahZaher/ruxbase/tools/types"
	"github.com/pocketbase/dbx"
)

const CorePluginSDKVersion = "1.0.0"

var (
	ErrPluginIncompatibleCoreVersion = errors.New("plugin incompatible: requires newer core SDK version")
	ErrPluginPrivilegeViolation      = errors.New("plugin isolation error: disallowed privileged capability requested")
	ErrPluginNotFound                = errors.New("plugin not found")
	ErrPluginAlreadyLoaded           = errors.New("plugin already loaded")
)

// PluginSpec defines a versioned extension package compatible with the Ruxbase Plugin SDK.
type PluginSpec struct {
	Id             string                  `db:"id" json:"id"`
	Name           string                  `db:"name" json:"name"`
	Version        string                  `db:"version" json:"version"`
	MinCoreVersion string                  `db:"min_core_version" json:"minCoreVersion"`
	Status         string                  `db:"status" json:"status"`
	Permissions    types.JSONArray[string] `db:"permissions" json:"permissions"`
	Created        types.DateTime          `db:"created" json:"created"`
	Updated        types.DateTime          `db:"updated" json:"updated"`
}

func parseSemverMajorMinor(v string) (int, int, error) {
	clean := strings.TrimPrefix(strings.TrimSpace(v), "v")
	parts := strings.Split(clean, ".")
	if len(parts) < 2 {
		return 0, 0, fmt.Errorf("invalid semver %q", v)
	}
	maj, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, err
	}
	min, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, err
	}
	return maj, min, nil
}

func validatePluginCompatibilityAndPermissions(spec *PluginSpec) error {
	if spec == nil || strings.TrimSpace(spec.Name) == "" || strings.TrimSpace(spec.Version) == "" {
		return errors.New("plugin name and version are required")
	}
	if spec.MinCoreVersion == "" {
		spec.MinCoreVersion = "0.1.0"
	}

	coreMaj, coreMin, err := parseSemverMajorMinor(CorePluginSDKVersion)
	if err != nil {
		return err
	}
	reqMaj, reqMin, err := parseSemverMajorMinor(spec.MinCoreVersion)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrPluginIncompatibleCoreVersion, err)
	}
	if reqMaj > coreMaj || (reqMaj == coreMaj && reqMin > coreMin) {
		return fmt.Errorf("%w: plugin %s requires >= %s (current core SDK %s)", ErrPluginIncompatibleCoreVersion, spec.Name, spec.MinCoreVersion, CorePluginSDKVersion)
	}

	// Enforce plugin isolation boundary: plugins cannot request raw host credentials or superuser DB bypasses
	forbiddenPerms := map[string]bool{
		"host_secrets": true,
		"superuser_db": true,
		"bypass_rls":   true,
		"os_exec":      true,
	}
	for _, perm := range spec.Permissions {
		if forbiddenPerms[strings.ToLower(strings.TrimSpace(perm))] {
			return fmt.Errorf("%w: %s", ErrPluginPrivilegeViolation, perm)
		}
	}

	return nil
}

// LoadPlugin validates compatibility and isolation boundaries and loads a plugin into _plugins.
func LoadPlugin(ctx context.Context, app App, spec *PluginSpec) (*PluginSpec, error) {
	if err := validatePluginCompatibilityAndPermissions(spec); err != nil {
		return nil, err
	}

	var exists bool
	_ = app.DB().NewQuery("SELECT EXISTS (SELECT 1 FROM {{_plugins}} WHERE [[name]] = {:name} AND [[status]] != 'unloaded')").
		WithContext(ctx).Bind(dbx.Params{"name": spec.Name}).Row(&exists)
	if exists {
		return nil, fmt.Errorf("%w: %s", ErrPluginAlreadyLoaded, spec.Name)
	}

	if spec.Id == "" {
		spec.Id = security.RandomStringWithAlphabet(15, "abcdefghijklmnopqrstuvwxyz0123456789")
	}
	spec.Status = "loaded"
	now := types.NowDateTime()
	spec.Created = now
	spec.Updated = now

	permsBytes, _ := json.Marshal(spec.Permissions)

	_, err := app.DB().NewQuery(`
		INSERT INTO {{_plugins}} ([[id]], [[name]], [[version]], [[min_core_version]], [[status]], [[permissions]], [[created]], [[updated]])
		VALUES ({:id}, {:name}, {:ver}, {:minVer}, {:status}, {:perms}::jsonb, {:created}, {:updated})
		ON CONFLICT ([[name]]) DO UPDATE SET
			[[version]] = EXCLUDED.[[version]],
			[[min_core_version]] = EXCLUDED.[[min_core_version]],
			[[status]] = 'loaded',
			[[permissions]] = EXCLUDED.[[permissions]],
			[[updated]] = now()
	`).WithContext(ctx).Bind(dbx.Params{
		"id":      spec.Id,
		"name":    spec.Name,
		"ver":     spec.Version,
		"minVer":  spec.MinCoreVersion,
		"status":  spec.Status,
		"perms":   string(permsBytes),
		"created": spec.Created.String(),
		"updated": spec.Updated.String(),
	}).Execute()

	if err != nil {
		return nil, err
	}
	return spec, nil
}

// UpgradePlugin validates a new plugin version & permissions and upgrades the loaded plugin atomically.
func UpgradePlugin(ctx context.Context, app App, spec *PluginSpec) (*PluginSpec, error) {
	if err := validatePluginCompatibilityAndPermissions(spec); err != nil {
		return nil, err
	}

	permsBytes, _ := json.Marshal(spec.Permissions)
	updated := &PluginSpec{}
	err := app.DB().NewQuery(`
		UPDATE {{_plugins}}
		SET [[version]] = {:ver},
		    [[min_core_version]] = {:minVer},
		    [[permissions]] = {:perms}::jsonb,
		    [[status]] = 'upgraded',
		    [[updated]] = now()
		WHERE [[name]] = {:name} AND [[status]] != 'unloaded'
		RETURNING [[id]], [[name]], [[version]], [[min_core_version]], [[status]], [[permissions]], [[created]], [[updated]]
	`).WithContext(ctx).Bind(dbx.Params{
		"name":   spec.Name,
		"ver":    spec.Version,
		"minVer": spec.MinCoreVersion,
		"perms":  string(permsBytes),
	}).One(updated)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrPluginNotFound
		}
		return nil, err
	}
	return updated, nil
}

// UnloadPlugin transitions a loaded/upgraded plugin cleanly to unloaded status.
func UnloadPlugin(ctx context.Context, app App, name string) error {
	res, err := app.DB().NewQuery(`
		UPDATE {{_plugins}}
		SET [[status]] = 'unloaded', [[updated]] = now()
		WHERE [[name]] = {:name} AND [[status]] != 'unloaded'
	`).WithContext(ctx).Bind(dbx.Params{"name": name}).Execute()
	if err != nil {
		return err
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return ErrPluginNotFound
	}
	return nil
}

// ListPlugins returns registered plugins and their lifecycle status.
func ListPlugins(ctx context.Context, app App) ([]*PluginSpec, error) {
	var plugins []*PluginSpec
	err := app.DB().NewQuery(`
		SELECT [[id]], [[name]], [[version]], [[min_core_version]], [[status]], [[permissions]], [[created]], [[updated]]
		FROM {{_plugins}}
		ORDER BY [[name]] ASC
	`).WithContext(ctx).All(&plugins)
	if plugins == nil {
		plugins = []*PluginSpec{}
	}
	return plugins, err
}
