package apis

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tools/router"
)

// bindStudioControlPlaneApi registers superuser-protected Studio diagnostics and
// optional self-hosted multi-project control plane endpoints.
func bindStudioControlPlaneApi(app core.App, rg *router.RouterGroup[*core.RequestEvent]) {
	studio := rg.Group("/studio").Bind(RequireSuperuserAuth())
	studio.GET("/overview", studioOverview)
	studio.GET("/jobs", studioJobsList)
	studio.POST("/jobs/{id}/replay", studioJobReplay)
	studio.GET("/audit", studioAuditLogs)

	cp := rg.Group("/control-plane").Bind(RequireSuperuserAuth())
	cp.GET("/projects", controlPlaneProjectsList)
	cp.POST("/projects", controlPlaneProjectCreate)
	cp.POST("/projects/{id}/status", controlPlaneProjectStatusUpdate)
}

func studioActorInfo(e *core.RequestEvent) (string, string, string) {
	var actorId, actorEmail string
	if e.Auth != nil {
		actorId = e.Auth.Id
		actorEmail = e.Auth.GetString("email")
	}
	return actorId, actorEmail, e.RealIP()
}

func studioOverview(e *core.RequestEvent) error {
	overview, err := core.GetStudioOverview(e.Request.Context(), e.App)
	if err != nil {
		return e.InternalServerError("Failed to gather Studio operational overview.", err)
	}
	return e.JSON(http.StatusOK, overview)
}

func studioJobsList(e *core.RequestEvent) error {
	queue := e.Request.URL.Query().Get("queue")
	status := e.Request.URL.Query().Get("status")
	limit, _ := strconv.Atoi(e.Request.URL.Query().Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	jobs, err := e.App.Jobs().ListJobs(queue, status, limit)
	if err != nil {
		return e.InternalServerError("Failed to list durable jobs.", err)
	}
	if jobs == nil {
		jobs = []*core.Job{}
	}

	return e.JSON(http.StatusOK, map[string]any{
		"count": len(jobs),
		"items": jobs,
	})
}

func studioJobReplay(e *core.RequestEvent) error {
	jobId := e.Request.PathValue("id")
	if jobId == "" {
		return e.BadRequestError("Job ID is required.", nil)
	}

	if err := e.App.Jobs().ReplayDeadJob(jobId); err != nil {
		return e.BadRequestError("Failed to replay dead job.", err)
	}

	actorId, actorEmail, ip := studioActorInfo(e)
	_, _ = core.RecordAdminAudit(
		e.Request.Context(),
		e.App,
		actorId,
		actorEmail,
		"replay",
		"durable_job",
		jobId,
		ip,
		map[string]any{"status": core.JobStatusPending},
	)

	return e.JSON(http.StatusOK, map[string]any{
		"replayed": true,
		"jobId":    jobId,
	})
}

func studioAuditLogs(e *core.RequestEvent) error {
	limit, _ := strconv.Atoi(e.Request.URL.Query().Get("limit"))
	logs, err := core.ListAdminAuditLogs(e.Request.Context(), e.App, limit)
	if err != nil {
		return e.InternalServerError("Failed to retrieve admin audit logs.", err)
	}
	if logs == nil {
		logs = []*core.AdminAuditLog{}
	}
	return e.JSON(http.StatusOK, map[string]any{
		"count": len(logs),
		"items": logs,
	})
}

func controlPlaneProjectsList(e *core.RequestEvent) error {
	projects, err := core.ListControlPlaneProjects(e.Request.Context(), e.App)
	if err != nil {
		return e.InternalServerError("Failed to list control plane projects.", err)
	}
	return e.JSON(http.StatusOK, map[string]any{
		"count": len(projects),
		"items": projects,
	})
}

func controlPlaneProjectCreate(e *core.RequestEvent) error {
	var body struct {
		Slug              string `json:"slug"`
		Name              string `json:"name"`
		Environment       string `json:"environment"`
		DBName            string `json:"dbName"`
		DBRole            string `json:"dbRole"`
		StorageQuotaBytes int64  `json:"storageQuotaBytes"`
		MaxConnections    int    `json:"maxConnections"`
	}
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("Invalid project JSON payload.", err)
	}

	actorId, actorEmail, ip := studioActorInfo(e)
	proj, err := core.CreateControlPlaneProject(e.Request.Context(), e.App, &core.ControlPlaneProject{
		Slug:              body.Slug,
		Name:              body.Name,
		Environment:       body.Environment,
		DBName:            body.DBName,
		DBRole:            body.DBRole,
		StorageQuotaBytes: body.StorageQuotaBytes,
		MaxConnections:    body.MaxConnections,
	}, actorId, actorEmail, ip)
	if err != nil {
		if errors.Is(err, core.ErrControlPlanePrivilegedRoleViolation) ||
			errors.Is(err, core.ErrControlPlaneInvalidIdentifier) ||
			errors.Is(err, core.ErrControlPlaneInvalidLifecycle) {
			return e.BadRequestError(err.Error(), err)
		}
		return e.InternalServerError("Failed to create control plane project.", err)
	}

	return e.JSON(http.StatusCreated, proj)
}

func controlPlaneProjectStatusUpdate(e *core.RequestEvent) error {
	id := e.Request.PathValue("id")
	var body struct {
		Status string `json:"status"`
	}
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("Invalid status payload.", err)
	}

	actorId, actorEmail, ip := studioActorInfo(e)
	proj, err := core.UpdateControlPlaneProjectStatus(e.Request.Context(), e.App, id, body.Status, actorId, actorEmail, ip)
	if err != nil {
		if errors.Is(err, core.ErrControlPlaneProjectNotFound) {
			return e.NotFoundError("Project not found.", err)
		}
		if errors.Is(err, core.ErrControlPlaneInvalidLifecycle) {
			return e.BadRequestError(err.Error(), err)
		}
		return e.InternalServerError("Failed to update project lifecycle.", err)
	}

	return e.JSON(http.StatusOK, proj)
}
