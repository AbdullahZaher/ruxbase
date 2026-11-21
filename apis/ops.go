package apis

import (
	"net/http"

	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tools/router"
)

// bindOpsApi registers superuser-only production observability, capacity, and alert endpoints.
func bindOpsApi(app core.App, rg *router.RouterGroup[*core.RequestEvent]) {
	sub := rg.Group("/ops").Bind(RequireSuperuserAuth())
	sub.GET("/metrics", opsMetrics)
	sub.GET("/capacity", opsCapacity)
	sub.GET("/alerts", opsAlerts)
}

func opsMetrics(e *core.RequestEvent) error {
	snap, err := core.CollectOpsMetrics(e.Request.Context(), e.App)
	if err != nil {
		return e.InternalServerError("Failed to collect operational metrics.", err)
	}

	if e.Request.URL.Query().Get("format") == "prometheus" {
		return e.String(http.StatusOK, core.FormatPrometheusMetrics(snap))
	}

	return e.JSON(http.StatusOK, snap)
}

func opsCapacity(e *core.RequestEvent) error {
	budget, err := core.EvaluateCapacityBudget(e.Request.Context(), e.App)
	if err != nil {
		return e.InternalServerError("Failed to evaluate connection capacity budget.", err)
	}

	return e.JSON(http.StatusOK, budget)
}

func opsAlerts(e *core.RequestEvent) error {
	snap, err := core.CollectOpsMetrics(e.Request.Context(), e.App)
	if err != nil {
		return e.InternalServerError("Failed to collect operational alerts.", err)
	}

	alerts := core.EvaluateOpsAlerts(snap)
	return e.JSON(http.StatusOK, map[string]any{
		"count":  len(alerts),
		"alerts": alerts,
	})
}
