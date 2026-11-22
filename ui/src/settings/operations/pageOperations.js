import { settingsSidebar } from "../settingsSidebar";

function statusLabelClass(status) {
    switch (String(status || "").toLowerCase()) {
        case "pass":
        case "completed":
        case "active":
        case "healthy":
            return "label success";
        case "warning":
        case "pending":
        case "running":
            return "label warning";
        case "danger":
        case "critical":
        case "blocked":
        case "failed":
        case "dead":
            return "label danger";
        default:
            return "label";
    }
}

function listSkeleton(isLoading, hasItems) {
    return t.div(
        {
            hidden: () => !isLoading() || hasItems(),
            className: "list-item",
        },
        t.div({ className: "skeleton-loader" }),
    );
}

function listEmpty(isLoading, hasItems, emptyText) {
    return t.div(
        {
            hidden: () => isLoading() || hasItems(),
            className: "list-item",
        },
        t.div({ className: "content block txt-hint" }, emptyText),
    );
}

export function pageOperations(route) {
    app.store.title = "Operations & Studio";

    const data = store({
        isLoading: false,
        replayingId: "",
        overview: null,
        jobs: [],
        audits: [],
        projects: [],
        error: "",
    });

    async function loadAll() {
        data.isLoading = true;
        data.error = "";

        try {
            const [overviewRes, jobsRes, auditsRes, projectsRes] = await Promise.all([
                app.pb.send("/api/studio/overview", { method: "GET" }),
                app.pb.send("/api/studio/jobs?limit=25", { method: "GET" }),
                app.pb.send("/api/studio/audit?limit=20", { method: "GET" }),
                app.pb.send("/api/control-plane/projects", { method: "GET" }),
            ]);
            data.overview = overviewRes || null;
            data.jobs = jobsRes?.items || [];
            data.audits = auditsRes?.items || [];
            data.projects = projectsRes?.items || [];
        } catch (err) {
            data.error = err?.message || "Failed to load Studio operations data.";
        } finally {
            data.isLoading = false;
        }
    }

    async function replayJob(jobId) {
        if (data.replayingId) {
            return;
        }
        data.replayingId = jobId;
        try {
            await app.pb.send(`/api/studio/jobs/${encodeURIComponent(jobId)}/replay`, {
                method: "POST",
            });
            app.toasts?.success?.(`Requeued dead job ${jobId}.`);
            await loadAll();
        } catch (err) {
            app.toasts?.error?.(err?.message || "Failed to replay job.");
        } finally {
            data.replayingId = "";
        }
    }

    loadAll();

    return t.div(
        { pbEvent: "pageOperations", className: "page page-operations-settings" },
        settingsSidebar(),
        t.div(
            { className: "page-content full-height" },
            t.header(
                { className: "page-header" },
                t.nav(
                    { className: "breadcrumbs" },
                    t.div({ className: "breadcrumb-item" }, "Settings"),
                    t.div({ className: "breadcrumb-item" }, () => app.store.title),
                ),
            ),
            t.div(
                { className: "wrapper m-b-base" },
                t.div(
                    { className: "grid" },
                    t.div(
                        { className: "col-lg-12" },
                        t.div(
                            { className: "flex gap-10 m-b-sm" },
                            t.div({ className: "txt-lg" }, "Platform operations, diagnostics and audit"),
                            app.components.refreshButton({
                                className: "btn sm transparent secondary circle tooltip-bottom",
                                onclick: loadAll,
                            }),
                        ),
                        () => {
                            if (data.error) {
                                return t.div({ className: "alert danger m-b-sm" }, data.error);
                            }
                            return null;
                        },
                        t.div(
                            { className: "list" },
                            t.div(
                                { className: "list-content" },
                                listSkeleton(() => data.isLoading, () => !!data.overview),
                                listEmpty(
                                    () => data.isLoading,
                                    () => !!data.overview,
                                    "No operational overview loaded.",
                                ),
                                () => {
                                    const ov = data.overview;
                                    if (!ov) {
                                        return null;
                                    }

                                    const releasePassed = !!ov.releaseBlockers?.passed;
                                    const pendingJobs = ov.metrics?.jobs?.pending ?? 0;
                                    const failedJobs = ov.metrics?.jobs?.failed ?? 0;
                                    const deadJobs = ov.metrics?.jobs?.dead ?? 0;

                                    return [
                                        t.div(
                                            { className: () => `list-item ${data.isLoading ? "faded" : ""}` },
                                            t.i({ className: "ri-shield-check-line", ariaHidden: true }),
                                            t.div(
                                                { className: "content" },
                                                t.span({ className: "txt-ellipsis" }, "Release readiness gate"),
                                                t.small(
                                                    { className: "txt-hint txt-nowrap" },
                                                    `(${ov.migrationsApplied ?? 0} migrations, ${
                                                        ov.collectionsCount ?? 0
                                                    } collections)`,
                                                ),
                                            ),
                                            t.span(
                                                { className: statusLabelClass(releasePassed ? "pass" : "blocked") },
                                                releasePassed ? "PASS" : "BLOCKED",
                                            ),
                                        ),
                                        t.div(
                                            { className: () => `list-item ${data.isLoading ? "faded" : ""}` },
                                            t.i({ className: "ri-database-2-line", ariaHidden: true }),
                                            t.div(
                                                { className: "content" },
                                                t.span({ className: "txt-ellipsis" }, "PostgreSQL connections"),
                                                t.small(
                                                    { className: "txt-hint txt-nowrap" },
                                                    `(${ov.metrics?.database?.activeBackends ?? 0} active backends)`,
                                                ),
                                            ),
                                            t.span({ className: "label" }, `${ov.buckets?.length ?? 0} buckets`),
                                        ),
                                        t.div(
                                            { className: () => `list-item ${data.isLoading ? "faded" : ""}` },
                                            t.i({ className: "ri-stack-line", ariaHidden: true }),
                                            t.div(
                                                { className: "content" },
                                                t.span({ className: "txt-ellipsis" }, "Durable job queue summary"),
                                                t.small(
                                                    { className: "txt-hint txt-nowrap" },
                                                    `(pending: ${pendingJobs}, failed: ${failedJobs}, dead: ${deadJobs})`,
                                                ),
                                            ),
                                            t.span(
                                                { className: statusLabelClass(deadJobs > 0 ? "dead" : "active") },
                                                deadJobs > 0 ? `${deadJobs} dead` : "healthy",
                                            ),
                                        ),
                                    ];
                                },
                            ),
                        ),
                    ),
                    t.div(
                        { className: "col-lg-12" },
                        t.div(
                            { className: "flex gap-10 m-b-sm" },
                            t.div(
                                { className: "txt-lg" },
                                () => `Active alerts (${data.overview?.alerts?.length || 0})`,
                            ),
                        ),
                        t.div(
                            { className: "list" },
                            t.div(
                                { className: "list-content" },
                                listSkeleton(() => data.isLoading, () => (data.overview?.alerts?.length || 0) > 0),
                                listEmpty(
                                    () => data.isLoading,
                                    () => (data.overview?.alerts?.length || 0) > 0,
                                    "No active operational alerts.",
                                ),
                                () => {
                                    const alerts = data.overview?.alerts || [];
                                    return alerts.map((a) =>
                                        t.div(
                                            { className: () => `list-item ${data.isLoading ? "faded" : ""}` },
                                            t.i({ className: "ri-alarm-warning-line", ariaHidden: true }),
                                            t.div(
                                                { className: "content" },
                                                t.span({ className: "txt-ellipsis" }, a.code),
                                                t.small({ className: "txt-hint" }, a.message),
                                            ),
                                            t.span(
                                                { className: statusLabelClass(a.severity) },
                                                String(a.severity || "info").toUpperCase(),
                                            ),
                                        )
                                    );
                                },
                            ),
                        ),
                    ),
                    t.div(
                        { className: "col-lg-12" },
                        t.div(
                            { className: "flex gap-10 m-b-sm" },
                            t.div({ className: "txt-lg" }, () => `Durable jobs (${data.jobs.length})`),
                        ),
                        t.div(
                            { className: "list" },
                            t.div(
                                { className: "list-content" },
                                listSkeleton(() => data.isLoading, () => data.jobs.length > 0),
                                listEmpty(() => data.isLoading, () => data.jobs.length > 0, "Queue is empty."),
                                () => {
                                    return data.jobs.map((j) =>
                                        t.div(
                                            { className: () => `list-item ${data.isLoading ? "faded" : ""}` },
                                            t.i({ className: "ri-cpu-line", ariaHidden: true }),
                                            t.div(
                                                { className: "content" },
                                                t.span(
                                                    { className: "txt-ellipsis", title: j.id },
                                                    `${j.task} (${j.id})`,
                                                ),
                                                t.small(
                                                    { className: "txt-hint txt-nowrap" },
                                                    `(attempts: ${j.attempts}/${j.maxRetries})`,
                                                ),
                                            ),
                                            t.span({ className: statusLabelClass(j.status) }, j.status),
                                            () => {
                                                if (j.status !== "dead") {
                                                    return null;
                                                }
                                                return t.nav(
                                                    { className: "actions" },
                                                    t.button(
                                                        {
                                                            type: "button",
                                                            ariaLabel: app.attrs.tooltip("Replay dead job"),
                                                            className: () =>
                                                                `btn sm circle secondary transparent ${
                                                                    data.replayingId === j.id ? "loading" : ""
                                                                }`,
                                                            disabled: () => !!data.replayingId,
                                                            onclick: () => replayJob(j.id),
                                                        },
                                                        t.i({ className: "ri-restart-line", ariaHidden: true }),
                                                    ),
                                                );
                                            },
                                        )
                                    );
                                },
                            ),
                        ),
                    ),
                    t.div(
                        { className: "col-lg-12" },
                        t.div(
                            { className: "flex gap-10 m-b-sm" },
                            t.div({ className: "txt-lg" }, () => `Admin audit trail (${data.audits.length})`),
                        ),
                        t.div(
                            { className: "list" },
                            t.div(
                                { className: "list-content" },
                                listSkeleton(() => data.isLoading, () => data.audits.length > 0),
                                listEmpty(
                                    () => data.isLoading,
                                    () => data.audits.length > 0,
                                    "No administrative audit entries recorded.",
                                ),
                                () => {
                                    return data.audits.map((log) =>
                                        t.div(
                                            { className: () => `list-item ${data.isLoading ? "faded" : ""}` },
                                            t.i({ className: "ri-history-line", ariaHidden: true }),
                                            t.div(
                                                { className: "content" },
                                                t.span(
                                                    { className: "txt-ellipsis" },
                                                    `${log.action} ${log.resource}${
                                                        log.targetId ? ` (${log.targetId})` : ""
                                                    }`,
                                                ),
                                                t.small(
                                                    { className: "txt-hint txt-nowrap" },
                                                    `${log.actorEmail || log.actorId || "system"} · ${log.created}`,
                                                ),
                                            ),
                                        )
                                    );
                                },
                            ),
                        ),
                    ),
                    t.div(
                        { className: "col-lg-12" },
                        t.div(
                            { className: "flex gap-10 m-b-sm" },
                            t.div({ className: "txt-lg" }, () => `Control plane projects (${data.projects.length})`),
                        ),
                        t.div(
                            { className: "list" },
                            t.div(
                                { className: "list-content" },
                                listSkeleton(() => data.isLoading, () => data.projects.length > 0),
                                listEmpty(
                                    () => data.isLoading,
                                    () => data.projects.length > 0,
                                    "Single-project mode active (no optional control plane projects registered).",
                                ),
                                () => {
                                    return data.projects.map((p) =>
                                        t.div(
                                            { className: () => `list-item ${data.isLoading ? "faded" : ""}` },
                                            t.i({ className: "ri-server-line", ariaHidden: true }),
                                            t.div(
                                                { className: "content" },
                                                t.span({ className: "txt-ellipsis" }, `${p.slug} (${p.environment})`),
                                                t.small(
                                                    { className: "txt-hint txt-nowrap" },
                                                    `DB=${p.dbName}, Role=${p.dbRole}`,
                                                ),
                                            ),
                                            t.span({ className: statusLabelClass(p.status) }, p.status),
                                        )
                                    );
                                },
                            ),
                        ),
                    ),
                ),
            ),
            t.footer({ className: "page-footer" }, app.components.credits()),
        ),
    );
}
