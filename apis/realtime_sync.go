package apis

import (
	"net/http"
	"strings"
	"time"

	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tools/router"
	"github.com/pocketbase/dbx"
)

// bindRealtimeSync registers delta sync and batch sync endpoints on the API router group.
func bindRealtimeSync(app core.App, rg *router.RouterGroup[*core.RequestEvent]) {
	sub := rg.Group("/realtime")
	sub.GET("/sync", realtimeDeltaSync)
	sub.POST("/sync", realtimeBatchSync)
}

func realtimeDeltaSync(e *core.RequestEvent) error {
	collectionName := strings.TrimSpace(e.Request.URL.Query().Get("collection"))
	if collectionName == "" {
		return e.BadRequestError("collection query parameter is required", nil)
	}

	col, err := e.App.FindCollectionByNameOrId(collectionName)
	if err != nil {
		return e.NotFoundError("Collection not found", err)
	}

	// Superusers bypass rules; non-superusers require non-nil ListRule
	if !e.HasSuperuserAuth() && col.ListRule == nil {
		return e.ForbiddenError("Only superusers can access this collection", nil)
	}

	sinceStr := strings.TrimSpace(e.Request.URL.Query().Get("since"))
	var sinceTime time.Time
	if sinceStr != "" {
		t, err := time.Parse(time.RFC3339Nano, sinceStr)
		if err != nil {
			t, err = time.Parse(time.RFC3339, sinceStr)
		}
		if err == nil {
			sinceTime = t.UTC()
		}
	}

	var syncTimestamp time.Time
	_ = e.App.DB().NewQuery("SELECT clock_timestamp()").Row(&syncTimestamp)
	if syncTimestamp.IsZero() {
		syncTimestamp = time.Now().UTC()
	}

	reqInfo, _ := e.RequestInfo()

	// Query updated records
	q := e.App.RecordQuery(col).Limit(500)
	if col.Fields.GetByName("updated") != nil {
		q.OrderBy("[[updated]] ASC")
		if !sinceTime.IsZero() {
			q.AndWhere(dbx.NewExp("[[updated]] >= {:since}", dbx.Params{"since": sinceTime.Format("2006-01-02 15:04:05.000Z07:00")}))
		}
	} else {
		q.OrderBy("[[id]] ASC")
	}

	var rawRecords []*core.Record
	if err := q.All(&rawRecords); err != nil {
		return e.InternalServerError("Failed to query records for sync", err)
	}

	// Filter accessible records
	accessibleRecords := make([]map[string]any, 0, len(rawRecords))
	for _, rec := range rawRecords {
		if e.HasSuperuserAuth() {
			accessibleRecords = append(accessibleRecords, rec.PublicExport())
			continue
		}
		if ok, _ := e.App.CanAccessRecord(rec, reqInfo, col.ListRule); ok {
			accessibleRecords = append(accessibleRecords, rec.PublicExport())
		}
	}

	// Query deleted record IDs from _realtime_events
	deletedIds := make([]string, 0)
	if !sinceTime.IsZero() {
		var rows []struct {
			RecordId string `db:"record_id"`
		}
		_ = e.App.DB().NewQuery(`
			SELECT DISTINCT [[record_id]]
			FROM {{_realtime_events}}
			WHERE [[collection_id]] = {:colId}
			  AND [[op]] IN ('delete', 'd')
			  AND [[created]] >= {:since}
		`).Bind(dbx.Params{
			"colId": col.Id,
			"since": sinceTime,
		}).All(&rows)

		for _, row := range rows {
			deletedIds = append(deletedIds, row.RecordId)
		}
	}

	return e.JSON(http.StatusOK, map[string]any{
		"collection": col.Name,
		"syncedAt":   syncTimestamp.UTC().Format(time.RFC3339Nano),
		"records":    accessibleRecords,
		"deleted":    deletedIds,
	})
}

type syncMutation struct {
	Action          string         `json:"action"` // "create", "update", "delete"
	Id              string         `json:"id"`
	Data            map[string]any `json:"data"`
	ClientUpdatedAt string         `json:"clientUpdatedAt"`
}

type batchSyncRequest struct {
	Collection string         `json:"collection"`
	Mutations  []syncMutation `json:"mutations"`
}

type mutationResult struct {
	Id      string         `json:"id"`
	Action  string         `json:"action"`
	Status  string         `json:"status"` // "applied", "conflict_server_won", "rejected"
	Current map[string]any `json:"current,omitempty"`
	Error   string         `json:"error,omitempty"`
}

func realtimeBatchSync(e *core.RequestEvent) error {
	form := new(batchSyncRequest)
	if err := e.BindBody(form); err != nil {
		return e.BadRequestError("Invalid batch sync payload", err)
	}

	col, err := e.App.FindCollectionByNameOrId(strings.TrimSpace(form.Collection))
	if err != nil {
		return e.NotFoundError("Collection not found", err)
	}

	var syncTimestamp time.Time
	_ = e.App.DB().NewQuery("SELECT clock_timestamp()").Row(&syncTimestamp)
	if syncTimestamp.IsZero() {
		syncTimestamp = time.Now().UTC()
	}

	reqInfo, _ := e.RequestInfo()

	applied := make([]mutationResult, 0)
	conflicts := make([]mutationResult, 0)

	err = e.App.RunInTransaction(func(txApp core.App) error {
		for _, m := range form.Mutations {
			action := strings.ToLower(strings.TrimSpace(m.Action))
			recordId := strings.TrimSpace(m.Id)

			var clientTime time.Time
			if m.ClientUpdatedAt != "" {
				if t, err := time.Parse(time.RFC3339Nano, m.ClientUpdatedAt); err == nil {
					clientTime = t.UTC()
				} else if t, err := time.Parse(time.RFC3339, m.ClientUpdatedAt); err == nil {
					clientTime = t.UTC()
				}
			}

			switch action {
			case "create":
				if !e.HasSuperuserAuth() && col.CreateRule == nil {
					conflicts = append(conflicts, mutationResult{
						Id:     recordId,
						Action: action,
						Status: "rejected",
						Error:  "Create rule does not allow creating records in this collection",
					})
					continue
				}

				// Check if record exists
				existing, err := txApp.FindRecordById(col, recordId)
				if err == nil && existing != nil {
					// Exists: LWW applies
					serverUpdated := existing.GetDateTime("updated").Time().UTC()
					if !clientTime.IsZero() && clientTime.Before(serverUpdated) {
						conflicts = append(conflicts, mutationResult{
							Id:      recordId,
							Action:  action,
							Status:  "conflict_server_won",
							Current: existing.PublicExport(),
						})
					} else {
						existing.Load(m.Data)
						if err := txApp.Save(existing); err != nil {
							conflicts = append(conflicts, mutationResult{
								Id:     recordId,
								Action: action,
								Status: "rejected",
								Error:  err.Error(),
							})
						} else {
							applied = append(applied, mutationResult{
								Id:     recordId,
								Action: action,
								Status: "applied",
							})
						}
					}
					continue
				}

				// New record create
				newRec := core.NewRecord(col)
				if recordId != "" {
					newRec.Set("id", recordId)
				}
				newRec.Load(m.Data)
				if err := txApp.Save(newRec); err != nil {
					conflicts = append(conflicts, mutationResult{
						Id:     recordId,
						Action: action,
						Status: "rejected",
						Error:  err.Error(),
					})
				} else {
					applied = append(applied, mutationResult{
						Id:     newRec.Id,
						Action: action,
						Status: "applied",
					})
				}

			case "update":
				if !e.HasSuperuserAuth() && col.UpdateRule == nil {
					conflicts = append(conflicts, mutationResult{
						Id:     recordId,
						Action: action,
						Status: "rejected",
						Error:  "Update rule does not allow updating records in this collection",
					})
					continue
				}

				existing, err := txApp.FindRecordById(col, recordId)
				if err != nil || existing == nil {
					conflicts = append(conflicts, mutationResult{
						Id:     recordId,
						Action: action,
						Status: "conflict_server_won",
						Error:  "Record not found on server",
					})
					continue
				}

				// Check permission
				if !e.HasSuperuserAuth() {
					if ok, _ := txApp.CanAccessRecord(existing, reqInfo, col.UpdateRule); !ok {
						conflicts = append(conflicts, mutationResult{
							Id:     recordId,
							Action: action,
							Status: "rejected",
							Error:  "Insufficient permissions to update record",
						})
						continue
					}
				}

				// LWW Conflict check
				serverUpdated := existing.GetDateTime("updated").Time().UTC()
				if !clientTime.IsZero() && clientTime.Before(serverUpdated) {
					conflicts = append(conflicts, mutationResult{
						Id:      recordId,
						Action:  action,
						Status:  "conflict_server_won",
						Current: existing.PublicExport(),
					})
				} else {
					existing.Load(m.Data)
					if err := txApp.Save(existing); err != nil {
						conflicts = append(conflicts, mutationResult{
							Id:     recordId,
							Action: action,
							Status: "rejected",
							Error:  err.Error(),
						})
					} else {
						applied = append(applied, mutationResult{
							Id:     recordId,
							Action: action,
							Status: "applied",
						})
					}
				}

			case "delete":
				if !e.HasSuperuserAuth() && col.DeleteRule == nil {
					conflicts = append(conflicts, mutationResult{
						Id:     recordId,
						Action: action,
						Status: "rejected",
						Error:  "Delete rule does not allow deleting records in this collection",
					})
					continue
				}

				existing, err := txApp.FindRecordById(col, recordId)
				if err != nil || existing == nil {
					// Already deleted -> idempotent success
					applied = append(applied, mutationResult{
						Id:     recordId,
						Action: action,
						Status: "applied",
					})
					continue
				}

				// Check permission
				if !e.HasSuperuserAuth() {
					if ok, _ := txApp.CanAccessRecord(existing, reqInfo, col.DeleteRule); !ok {
						conflicts = append(conflicts, mutationResult{
							Id:     recordId,
							Action: action,
							Status: "rejected",
							Error:  "Insufficient permissions to delete record",
						})
						continue
					}
				}

				// LWW Conflict check
				serverUpdated := existing.GetDateTime("updated").Time().UTC()
				if !clientTime.IsZero() && clientTime.Before(serverUpdated) {
					conflicts = append(conflicts, mutationResult{
						Id:      recordId,
						Action:  action,
						Status:  "conflict_server_won",
						Current: existing.PublicExport(),
					})
				} else {
					if err := txApp.Delete(existing); err != nil {
						conflicts = append(conflicts, mutationResult{
							Id:     recordId,
							Action: action,
							Status: "rejected",
							Error:  err.Error(),
						})
					} else {
						applied = append(applied, mutationResult{
							Id:     recordId,
							Action: action,
							Status: "applied",
						})
					}
				}
			}
		}
		return nil
	})

	if err != nil {
		return e.InternalServerError("Failed to execute sync batch", err)
	}

	return e.JSON(http.StatusOK, map[string]any{
		"collection": col.Name,
		"syncedAt":   syncTimestamp.UTC().Format(time.RFC3339Nano),
		"applied":    applied,
		"conflicts":  conflicts,
	})
}
