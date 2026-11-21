package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/AbdullahZaher/ruxbase/tools/security"
	"github.com/AbdullahZaher/ruxbase/tools/types"
	"github.com/pocketbase/dbx"
)

var (
	ErrControlPlanePrivilegedRoleViolation = errors.New("project db_role cannot reference privileged or shared system roles")
	ErrControlPlaneInvalidIdentifier       = errors.New("invalid project slug, db_name, or db_role identifier")
	ErrControlPlaneInvalidLifecycle        = errors.New("invalid project status or environment")
	ErrControlPlaneProjectNotFound         = errors.New("control plane project not found")

	cpIdentifierRegex = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,48}$`)
)

// AdminAuditLog records an immutable administrative mutation or security-sensitive action.
type AdminAuditLog struct {
	Id         string             `db:"id" json:"id"`
	ActorId    string             `db:"actor_id" json:"actorId"`
	ActorEmail string             `db:"actor_email" json:"actorEmail"`
	Action     string             `db:"action" json:"action"`
	Resource   string             `db:"resource" json:"resource"`
	TargetId   string             `db:"target_id" json:"targetId"`
	Details    types.JSONMap[any] `db:"details" json:"details,omitempty"`
	IPAddress  string             `db:"ip_address" json:"ipAddress"`
	Created    types.DateTime     `db:"created" json:"created"`
}

// ControlPlaneProject represents an optional isolated tenant/project registered in the self-hosted control plane.
type ControlPlaneProject struct {
	Id                string         `db:"id" json:"id"`
	Slug              string         `db:"slug" json:"slug"`
	Name              string         `db:"name" json:"name"`
	Environment       string         `db:"environment" json:"environment"`
	DBName            string         `db:"db_name" json:"dbName"`
	DBRole            string         `db:"db_role" json:"dbRole"`
	Status            string         `db:"status" json:"status"`
	StorageQuotaBytes int64          `db:"storage_quota_bytes" json:"storageQuotaBytes"`
	MaxConnections    int            `db:"max_connections" json:"maxConnections"`
	Created           types.DateTime `db:"created" json:"created"`
	Updated           types.DateTime `db:"updated" json:"updated"`
}

// StudioBucketSummary summarizes storage bucket usage for Studio operational views.
type StudioBucketSummary struct {
	Id         string `db:"id" json:"id"`
	Name       string `db:"name" json:"name"`
	Public     bool   `db:"public" json:"public"`
	Objects    int    `db:"objects" json:"objects"`
	TotalBytes int64  `db:"total_bytes" json:"totalBytes"`
}

// StudioOverview aggregates operational diagnostics for Ruxbase Studio.
type StudioOverview struct {
	MigrationsApplied int                   `json:"migrationsApplied"`
	CollectionsCount  int                   `json:"collectionsCount"`
	Metrics           *OpsMetricsSnapshot   `json:"metrics"`
	Alerts            []OpsAlert            `json:"alerts"`
	ReleaseBlockers   *ReleaseBlockerReport `json:"releaseBlockers"`
	Buckets           []StudioBucketSummary `json:"buckets"`
}

// RecordAdminAudit inserts an immutable audit record and emits a structured log event.
func RecordAdminAudit(
	ctx context.Context,
	app App,
	actorId string,
	actorEmail string,
	action string,
	resource string,
	targetId string,
	ipAddress string,
	details map[string]any,
) (*AdminAuditLog, error) {
	entry := &AdminAuditLog{
		Id:         security.RandomStringWithAlphabet(15, "abcdefghijklmnopqrstuvwxyz0123456789"),
		ActorId:    actorId,
		ActorEmail: actorEmail,
		Action:     action,
		Resource:   resource,
		TargetId:   targetId,
		Details:    details,
		IPAddress:  ipAddress,
		Created:    types.NowDateTime(),
	}

	detailsBytes, _ := json.Marshal(entry.Details)

	_, err := app.DB().NewQuery(`
		INSERT INTO {{_admin_audit_logs}} (
			[[id]], [[actor_id]], [[actor_email]], [[action]], [[resource]], [[target_id]], [[details]], [[ip_address]], [[created]]
		) VALUES (
			{:id}, {:actorId}, {:actorEmail}, {:action}, {:resource}, {:targetId}, {:details}::jsonb, {:ip}, {:created}
		)
	`).WithContext(ctx).Bind(dbx.Params{
		"id":         entry.Id,
		"actorId":    entry.ActorId,
		"actorEmail": entry.ActorEmail,
		"action":     entry.Action,
		"resource":   entry.Resource,
		"targetId":   entry.TargetId,
		"details":    string(detailsBytes),
		"ip":         entry.IPAddress,
		"created":    entry.Created.String(),
	}).Execute()

	if err != nil {
		return nil, fmt.Errorf("failed to record admin audit log: %w", err)
	}

	app.Logger().Info(
		"Admin audit event recorded",
		slog.String("action", entry.Action),
		slog.String("resource", entry.Resource),
		slog.String("targetId", entry.TargetId),
		slog.String("actor", entry.ActorEmail),
	)

	return entry, nil
}

// ListAdminAuditLogs retrieves recent administrative audit entries.
func ListAdminAuditLogs(ctx context.Context, app App, limit int) ([]*AdminAuditLog, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	var logs []*AdminAuditLog
	err := app.DB().NewQuery(`
		SELECT [[id]], [[actor_id]], [[actor_email]], [[action]], [[resource]], [[target_id]], [[details]], [[ip_address]], [[created]]
		FROM {{_admin_audit_logs}}
		ORDER BY [[created]] DESC
		LIMIT {:limit}
	`).WithContext(ctx).Bind(dbx.Params{"limit": limit}).All(&logs)

	return logs, err
}

// GetStudioOverview gathers operational diagnostics, migrations, buckets, alerts, and blocker health.
func GetStudioOverview(ctx context.Context, app App) (*StudioOverview, error) {
	snap, err := CollectOpsMetrics(ctx, app)
	if err != nil {
		return nil, err
	}

	alerts := EvaluateOpsAlerts(snap)
	blockers, _ := CheckReleaseBlockers(ctx, app)

	var migrationsCount int
	_ = app.DB().NewQuery("SELECT count(*) FROM {{_migrations}}").WithContext(ctx).Row(&migrationsCount)

	var collectionsCount int
	_ = app.DB().NewQuery("SELECT count(*) FROM {{_collections}}").WithContext(ctx).Row(&collectionsCount)

	var buckets []StudioBucketSummary
	_ = app.DB().NewQuery(`
		SELECT b.[[id]], b.[[name]], b.[[public]],
		       count(o.[[id]]) AS [[objects]],
		       COALESCE(sum(o.[[size]]), 0) AS [[total_bytes]]
		FROM {{_storage_buckets}} b
		LEFT JOIN {{_storage_objects}} o ON o.[[bucket_id]] = b.[[id]]
		GROUP BY b.[[id]], b.[[name]], b.[[public]]
		ORDER BY b.[[name]] ASC
	`).WithContext(ctx).All(&buckets)

	if buckets == nil {
		buckets = []StudioBucketSummary{}
	}

	return &StudioOverview{
		MigrationsApplied: migrationsCount,
		CollectionsCount:  collectionsCount,
		Metrics:           snap,
		Alerts:            alerts,
		ReleaseBlockers:   blockers,
		Buckets:           buckets,
	}, nil
}

// CreateControlPlaneProject registers a project with strict database, role, and quota isolation checks.
func CreateControlPlaneProject(
	ctx context.Context,
	app App,
	project *ControlPlaneProject,
	actorId string,
	actorEmail string,
	ipAddress string,
) (*ControlPlaneProject, error) {
	if project == nil || project.Name == "" {
		return nil, errors.New("project name is required")
	}

	project.Slug = strings.ToLower(strings.TrimSpace(project.Slug))
	project.DBName = strings.ToLower(strings.TrimSpace(project.DBName))
	project.DBRole = strings.ToLower(strings.TrimSpace(project.DBRole))

	if !cpIdentifierRegex.MatchString(project.Slug) ||
		!cpIdentifierRegex.MatchString(project.DBName) ||
		!cpIdentifierRegex.MatchString(project.DBRole) {
		return nil, ErrControlPlaneInvalidIdentifier
	}

	// Security boundary (W09-05): prohibit shared system or privileged PostgreSQL roles
	forbiddenRoles := map[string]bool{
		"postgres":              true,
		"admin":                 true,
		"ruxbase_app":           true,
		"ruxbase_migrator":      true,
		"ruxbase_provisioner":   true,
		"ruxbase_anon":          true,
		"ruxbase_authenticated": true,
	}
	if forbiddenRoles[project.DBRole] || strings.HasPrefix(project.DBRole, "pg_") {
		return nil, ErrControlPlanePrivilegedRoleViolation
	}

	if project.Environment == "" {
		project.Environment = "development"
	}
	switch project.Environment {
	case "development", "staging", "production":
	default:
		return nil, ErrControlPlaneInvalidLifecycle
	}

	if project.Status == "" {
		project.Status = "active"
	}
	switch project.Status {
	case "active", "suspended", "archived":
	default:
		return nil, ErrControlPlaneInvalidLifecycle
	}

	if project.StorageQuotaBytes <= 0 {
		project.StorageQuotaBytes = 1024 * 1024 * 1024 // 1GB default
	}
	if project.MaxConnections <= 0 {
		project.MaxConnections = 20
	}
	if project.Id == "" {
		project.Id = security.RandomStringWithAlphabet(15, "abcdefghijklmnopqrstuvwxyz0123456789")
	}

	now := types.NowDateTime()
	project.Created = now
	project.Updated = now

	_, err := app.DB().NewQuery(`
		INSERT INTO {{_control_plane_projects}} (
			[[id]], [[slug]], [[name]], [[environment]], [[db_name]], [[db_role]], [[status]], [[storage_quota_bytes]], [[max_connections]], [[created]], [[updated]]
		) VALUES (
			{:id}, {:slug}, {:name}, {:env}, {:dbName}, {:dbRole}, {:status}, {:quota}, {:maxConns}, {:created}, {:updated}
		)
	`).WithContext(ctx).Bind(dbx.Params{
		"id":       project.Id,
		"slug":     project.Slug,
		"name":     project.Name,
		"env":      project.Environment,
		"dbName":   project.DBName,
		"dbRole":   project.DBRole,
		"status":   project.Status,
		"quota":    project.StorageQuotaBytes,
		"maxConns": project.MaxConnections,
		"created":  project.Created.String(),
		"updated":  project.Updated.String(),
	}).Execute()

	if err != nil {
		return nil, fmt.Errorf("failed to create control plane project: %w", err)
	}

	_, _ = RecordAdminAudit(ctx, app, actorId, actorEmail, "create", "control_plane_project", project.Id, ipAddress, map[string]any{
		"slug":        project.Slug,
		"environment": project.Environment,
		"dbName":      project.DBName,
		"dbRole":      project.DBRole,
	})

	return project, nil
}

// ListControlPlaneProjects retrieves all projects registered in the optional control plane inventory.
func ListControlPlaneProjects(ctx context.Context, app App) ([]*ControlPlaneProject, error) {
	var projects []*ControlPlaneProject
	err := app.DB().NewQuery(`
		SELECT [[id]], [[slug]], [[name]], [[environment]], [[db_name]], [[db_role]], [[status]], [[storage_quota_bytes]], [[max_connections]], [[created]], [[updated]]
		FROM {{_control_plane_projects}}
		ORDER BY [[created]] DESC
	`).WithContext(ctx).All(&projects)

	if projects == nil {
		projects = []*ControlPlaneProject{}
	}
	return projects, err
}

// UpdateControlPlaneProjectStatus updates project lifecycle status with audit logging.
func UpdateControlPlaneProjectStatus(
	ctx context.Context,
	app App,
	idOrSlug string,
	status string,
	actorId string,
	actorEmail string,
	ipAddress string,
) (*ControlPlaneProject, error) {
	switch status {
	case "active", "suspended", "archived":
	default:
		return nil, ErrControlPlaneInvalidLifecycle
	}

	proj := &ControlPlaneProject{}
	err := app.DB().NewQuery(`
		UPDATE {{_control_plane_projects}}
		SET [[status]] = {:status}, [[updated]] = now()
		WHERE [[id]] = {:val} OR [[slug]] = {:val}
		RETURNING [[id]], [[slug]], [[name]], [[environment]], [[db_name]], [[db_role]], [[status]], [[storage_quota_bytes]], [[max_connections]], [[created]], [[updated]]
	`).WithContext(ctx).Bind(dbx.Params{
		"val":    idOrSlug,
		"status": status,
	}).One(proj)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrControlPlaneProjectNotFound
		}
		return nil, err
	}

	_, _ = RecordAdminAudit(ctx, app, actorId, actorEmail, "update_status", "control_plane_project", proj.Id, ipAddress, map[string]any{
		"slug":   proj.Slug,
		"status": status,
	})

	return proj, nil
}
