package apis

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tools/router"
	"github.com/AbdullahZaher/ruxbase/tools/routine"
	"github.com/AbdullahZaher/ruxbase/tools/security"
	"github.com/AbdullahZaher/ruxbase/tools/subscriptions"
	"golang.org/x/sync/errgroup"
)

const (
	clusterTopicRealtimeBroadcast = "rt_broadcast"
	clusterTopicRealtimePresence  = "rt_presence"
)

// bindRealtimeBroadcastPresence registers broadcast and presence endpoints on the API router group.
func bindRealtimeBroadcastPresence(app core.App, rg *router.RouterGroup[*core.RequestEvent]) {
	sub := rg.Group("/realtime")
	sub.POST("/broadcast", realtimeBroadcast)
	sub.POST("/presence", realtimePresence)
}

type broadcastRequestForm struct {
	Channel string `json:"channel"`
	Event   string `json:"event"`
	Payload any    `json:"payload"`
}

func realtimeBroadcast(e *core.RequestEvent) error {
	form := new(broadcastRequestForm)
	if err := e.BindBody(form); err != nil {
		return e.BadRequestError("Invalid broadcast request payload", err)
	}

	form.Channel = strings.TrimSpace(form.Channel)
	form.Event = strings.TrimSpace(form.Event)
	if form.Channel == "" || form.Event == "" {
		return e.BadRequestError("channel and event are required fields", nil)
	}

	senderId := ""
	tenantId := e.Request.Header.Get("X-Tenant-Id")
	if e.Auth != nil {
		senderId = e.Auth.Id
		if tenantId == "" {
			tenantId = e.Auth.GetString(core.TenantIdField)
		}
	}

	dataBytes, err := json.Marshal(map[string]any{
		"event":   form.Event,
		"channel": form.Channel,
		"payload": form.Payload,
		"sender":  senderId,
	})
	if err != nil {
		return e.InternalServerError("Failed to encode broadcast data", err)
	}

	topic := "broadcast:" + form.Channel
	eventId := fmt.Sprintf("%d_%s", time.Now().UnixMilli(), security.RandomString(10))
	msg := subscriptions.Message{
		Id:   eventId,
		Name: topic,
		Data: dataBytes,
	}

	// Dispatch locally to subscribers of this broadcast topic
	_ = realtimeDispatchToTopic(e.App, topic, tenantId, msg)

	// Propagate across cluster nodes
	if e.App.Cluster().Enabled() {
		rawPayload, _ := json.Marshal(form.Payload)
		clusterMsg := fmt.Sprintf("%s|%s|%s|%s|%s", form.Channel, form.Event, senderId, tenantId, string(rawPayload))
		_ = e.App.Cluster().Notify(clusterTopicRealtimeBroadcast, clusterMsg)
	}

	return e.JSON(http.StatusOK, map[string]any{"sent": true, "channel": form.Channel})
}

type presenceRequestForm struct {
	Channel  string         `json:"channel"`
	ClientId string         `json:"clientId"`
	Action   string         `json:"action"` // "track" or "untrack"
	State    map[string]any `json:"state"`
}

func realtimePresence(e *core.RequestEvent) error {
	form := new(presenceRequestForm)
	if err := e.BindBody(form); err != nil {
		return e.BadRequestError("Invalid presence request payload", err)
	}

	form.Channel = strings.TrimSpace(form.Channel)
	form.ClientId = strings.TrimSpace(form.ClientId)
	form.Action = strings.ToLower(strings.TrimSpace(form.Action))

	if form.Channel == "" || form.ClientId == "" {
		return e.BadRequestError("channel and clientId are required fields", nil)
	}
	if form.Action == "" {
		form.Action = "track"
	}

	userId := ""
	tenantId := e.Request.Header.Get("X-Tenant-Id")
	if e.Auth != nil {
		userId = e.Auth.Id
		if tenantId == "" {
			tenantId = e.Auth.GetString(core.TenantIdField)
		}
	}

	switch form.Action {
	case "track":
		e.App.Presence().Track(form.Channel, form.ClientId, userId, tenantId, form.State)
	case "untrack":
		e.App.Presence().Untrack(form.Channel, form.ClientId)
	}

	presenceList := e.App.Presence().List(form.Channel)
	dataBytes, err := json.Marshal(map[string]any{
		"event":    "sync",
		"channel":  form.Channel,
		"presence": presenceList,
	})
	if err == nil {
		topic := "presence:" + form.Channel
		eventId := fmt.Sprintf("%d_%s", time.Now().UnixMilli(), security.RandomString(10))
		msg := subscriptions.Message{
			Id:   eventId,
			Name: topic,
			Data: dataBytes,
		}
		_ = realtimeDispatchToTopic(e.App, topic, tenantId, msg)

		if e.App.Cluster().Enabled() {
			clusterMsg := fmt.Sprintf("%s|%s", form.Channel, string(dataBytes))
			_ = e.App.Cluster().Notify(clusterTopicRealtimePresence, clusterMsg)
		}
	}

	return e.JSON(http.StatusOK, map[string]any{
		"channel":  form.Channel,
		"presence": presenceList,
	})
}

// realtimeCleanupClientPresence cleans up presence for a disconnected client and emits sync events
func realtimeCleanupClientPresence(app core.App, clientId string) {
	if app.Presence() == nil {
		return
	}
	affected := app.Presence().UntrackSession(clientId)
	for _, channel := range affected {
		presenceList := app.Presence().List(channel)
		dataBytes, err := json.Marshal(map[string]any{
			"event":    "sync",
			"channel":  channel,
			"presence": presenceList,
		})
		if err == nil {
			topic := "presence:" + channel
			eventId := fmt.Sprintf("%d_%s", time.Now().UnixMilli(), security.RandomString(10))
			msg := subscriptions.Message{
				Id:   eventId,
				Name: topic,
				Data: dataBytes,
			}
			_ = realtimeDispatchToTopic(app, topic, "", msg)

			if app.Cluster().Enabled() {
				clusterMsg := fmt.Sprintf("%s|%s", channel, string(dataBytes))
				_ = app.Cluster().Notify(clusterTopicRealtimePresence, clusterMsg)
			}
		}
	}
}

// realtimeDispatchToTopic delivers a message to all connected clients subscribed to the topic.
func realtimeDispatchToTopic(app core.App, topic string, tenantId string, msg subscriptions.Message) error {
	chunks := app.SubscriptionsBroker().ChunkedClients(clientsChunkSize)
	if len(chunks) == 0 {
		return nil
	}

	group := new(errgroup.Group)

	for _, chunk := range chunks {
		group.Go(routine.SafeWrap(func() error {
			for _, client := range chunk {
				if client.HasSubscription(topic) || client.HasSubscription(topic+"/*") {
					if tenantId != "" {
						clientTenant, _ := client.Get(RealtimeClientTenantKey).(string)
						if clientTenant != "" && clientTenant != tenantId {
							continue
						}
					}
					c := client
					routine.FireAndForget(func() {
						c.Send(msg)
					})
				}
			}
			return nil
		}))
	}

	return group.Wait()
}
