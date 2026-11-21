import { settingsSidebar } from "../settingsSidebar";

export function pageOperations(route) {
    app.store.title = "Operations & Studio";

    const data = store({
        isLoading: false,
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
        try {
            await app.pb.send(`/api/studio/jobs/${encodeURIComponent(jobId)}/replay`, {
                method: "POST",
            });
            app.toasts?.success?.(`Requeued dead job ${jobId}.`);
            await loadAll();
        } catch (err) {
            app.toasts?.error?.(err?.message || "Failed to replay job.");
        }
    }

    loadAll();

    return t.div(
        { pbEvent: "pageOperations", className: "page" },
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
                    { className: "flex gap-10 m-b-sm" },
                    t.div({ className: "txt-lg" }, "Platform Operations, Diagnostics & Audit"),
                    app.components.refreshButton({
                        className: "btn sm transparent secondary circle",
                        onclick: loadAll,
                    }),
                ),
                () => {
                    if (data.error) {
                        return t.div({ className: "alert danger m-b-sm" }, data.error);
                    }
                    if (data.isLoading && !data.overview) {
                        return t.span({ className: "loader" });
                    }
                    if (!data.overview) {
                        return t.div({ className: "txt-hint" }, "No operational overview loaded.");
                    }

                    const ov = data.overview;
                    return t.div(
                        { className: "grid grid-cols-1 gap-base" },
                        t.div(
                            { className: "card p-base" },
                            t.div({ className: "txt-bold m-b-xs" }, "Cluster & PostgreSQL Health"),
                            t.div(
                                { className: "txt-sm" },
                                `Migrations applied: ${ov.migrationsApplied} | Collections: ${ov.collectionsCount} | Active backends: ${
                                    ov.metrics?.database?.activeBackends ?? 0
                                } | Release gate: ${ov.releaseBlockers?.passed ? "PASS" : "BLOCKED"}`,
                            ),
                            t.div(
                                { className: "txt-sm txt-hint m-t-xs" },
                                `Jobs: pending=${ov.metrics?.jobs?.pending ?? 0}, failed=${
                                    ov.metrics?.jobs?.failed ?? 0
                                }, dead=${ov.metrics?.jobs?.dead ?? 0} | Storage buckets=${ov.buckets?.length ?? 0}`,
                            ),
                        ),
                        t.div(
                            { className: "card p-base" },
                            t.div({ className: "txt-bold m-b-xs" }, `Active Alerts (${ov.alerts?.length || 0})`),
                            () => {
                                if (!ov.alerts || ov.alerts.length === 0) {
                                    return t.div({ className: "txt-sm txt-hint" }, "No active operational alerts.");
                                }
                                return ov.alerts.map((a) =>
                                    t.div(
                                        { className: "txt-sm m-b-xs" },
                                        `[${String(a.severity).toUpperCase()}] ${a.code}: ${a.message}`,
                                    )
                                );
                            },
                        ),
                        t.div(
                            { className: "card p-base" },
                            t.div({ className: "txt-bold m-b-xs" }, `Durable Jobs (${data.jobs.length})`),
                            () => {
                                if (data.jobs.length === 0) {
                                    return t.div({ className: "txt-sm txt-hint" }, "Queue is empty.");
                                }
                                return data.jobs.map((j) =>
                                    t.div(
                                        { className: "flex items-center justify-between gap-10 m-b-xs txt-sm" },
                                        t.span(
                                            {},
                                            `${j.id} (${j.task}) — ${j.status} (attempts: ${j.attempts}/${j.maxRetries})`,
                                        ),
                                        () => {
                                            if (j.status === "dead") {
                                                return t.button(
                                                    {
                                                        type: "button",
                                                        className: "btn sm secondary",
                                                        onclick: () => replayJob(j.id),
                                                    },
                                                    "Replay",
                                                );
                                            }
                                            return null;
                                        },
                                    )
                                );
                            },
                        ),
                        t.div(
                            { className: "card p-base" },
                            t.div({ className: "txt-bold m-b-xs" }, `Admin Audit Trail (${data.audits.length})`),
                            () => {
                                if (data.audits.length === 0) {
                                    return t.div(
                                        { className: "txt-sm txt-hint" },
                                        "No administrative audit entries recorded.",
                                    );
                                }
                                return data.audits.map((log) =>
                                    t.div(
                                        { className: "txt-sm m-b-xs" },
                                        `${log.created} — ${
                                            log.actorEmail || log.actorId || "system"
                                        }: ${log.action} ${log.resource} (${log.targetId})`,
                                    )
                                );
                            },
                        ),
                        t.div(
                            { className: "card p-base" },
                            t.div({ className: "txt-bold m-b-xs" }, `Control Plane Projects (${data.projects.length})`),
                            () => {
                                if (data.projects.length === 0) {
                                    return t.div(
                                        { className: "txt-sm txt-hint" },
                                        "Single-project mode active (no optional control plane projects registered).",
                                    );
                                }
                                return data.projects.map((p) =>
                                    t.div(
                                        { className: "txt-sm m-b-xs" },
                                        `${p.slug} (${p.environment}) — DB=${p.dbName}, Role=${p.dbRole}, Status=${p.status}`,
                                    )
                                );
                            },
                        ),
                    );
                },
            ),
            t.footer({ className: "page-footer" }, app.components.credits()),
        ),
    );
}
