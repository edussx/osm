package catalog

import (
	"reflect"
	"strings"
	"time"

	a "github.com/openservicemesh/osm/pkg/announcements"
	"github.com/openservicemesh/osm/pkg/k8s/events"
	"github.com/openservicemesh/osm/pkg/metricsstore"
)

const (
	// maxBroadcastDeadlineTime is the max time we will delay a global proxy update
	// if multiple events that would trigger it get coalesced over time.
	maxBroadcastDeadlineTime = 15 * time.Second
	// maxGraceDeadlineTime is the time we will wait for an additional global proxy update
	// trigger if we just received one.
	maxGraceDeadlineTime = 3 * time.Second
)

// isDeltaUpdate assesses and returns if a pubsub message contains an actual delta in config
func isDeltaUpdate(psubMsg events.PubSubMessage) bool {
	return !(strings.HasSuffix(psubMsg.AnnouncementType.String(), "updated") &&
		reflect.DeepEqual(psubMsg.OldObj, psubMsg.NewObj))
}

func (mc *MeshCatalog) dispatcher() {
	// This will be finely tuned in near future, we can instrument other modules
	// to take ownership of certain events, and just notify dispatcher through
	// ScheduleBroadcastUpdate announcement type
	subChannel := events.Subscribe(
		a.ScheduleProxyBroadcast,                              // Other modules requesting a global envoy update
		a.EndpointAdded, a.EndpointDeleted, a.EndpointUpdated, // endpoint
		a.NamespaceAdded, a.NamespaceDeleted, a.NamespaceUpdated, // namespace
		a.PodAdded, a.PodDeleted, a.PodUpdated, // pod
		a.RouteGroupAdded, a.RouteGroupDeleted, a.RouteGroupUpdated, // routegroup
		a.ServiceAdded, a.ServiceDeleted, a.ServiceUpdated, // service
		a.MultiClusterServiceAdded, a.MultiClusterServiceDeleted, a.MultiClusterServiceUpdated, // Multicluster Service
		a.ServiceAccountAdded, a.ServiceAccountDeleted, a.ServiceAccountUpdated, // serviceaccount
		a.TrafficSplitAdded, a.TrafficSplitDeleted, a.TrafficSplitUpdated, // traffic split
		a.TrafficTargetAdded, a.TrafficTargetDeleted, a.TrafficTargetUpdated, // traffic target
		a.IngressAdded, a.IngressDeleted, a.IngressUpdated, // Ingress
		a.TCPRouteAdded, a.TCPRouteDeleted, a.TCPRouteUpdated, // TCProute
		a.EgressAdded, a.EgressDeleted, a.EgressUpdated, // Egress
		a.IngressBackendAdded, a.IngressBackendDeleted, a.IngressBackendUpdated, // IngressBackend
	)

	// State and channels for event-coalescing
	broadcastScheduled := false
	chanMovingDeadline := make(<-chan time.Time)
	chanMaxDeadline := make(<-chan time.Time)

	// tl;dr "When a broadcast request is scheduled, we will wait (3s) in case we receive another broadcast request
	// during this delay that can be coalesced (and restart the (3s) count if we do) up to a maximum of (15s) delay"

	// When there is no broadcast scheduled (broadcastScheduled == false) we start a max deadline (15s)
	// and a moving deadline (3s) timers.
	// The max deadline (15s) is the guaranteed hard max time we will wait till the next
	// envoy global broadcast is actually published to update all envoys.
	// Max deadline is used to limit the amount of times we might delay issuing the update, as new broadcast
	// requests can keep on delaying the moving deadline potentially forever.
	// The moving deadline resets if a new delta/change/request is detected in the next (3s). This is used to coalesce updates
	// and avoid issuing global envoy reconfiguration at large if new updates are meant to be received shortly after.
	// Either deadline will trigger the broadcast, whichever happens first, given previous conditions.
	// This mechanism is reset when the broadcast is published.

	for {
		select {
		case message := <-subChannel:
			psubMessage, ok := message.(events.PubSubMessage)
			if !ok {
				log.Error().Msgf("Error casting PubSubMessage: %v", psubMessage)
				continue
			}

			// Identify if this is an actual delta, or just resync
			delta := isDeltaUpdate(psubMessage)
			log.Debug().Msgf("[Pubsub] %s - delta: %v", psubMessage.AnnouncementType, delta)

			// Schedule an envoy broadcast update if we either:
			// - detected a config delta
			// - another module requested a broadcast through ScheduleProxyBroadcast
			if delta || psubMessage.AnnouncementType == a.ScheduleProxyBroadcast {
				if !broadcastScheduled {
					broadcastScheduled = true
					chanMaxDeadline = time.After(maxBroadcastDeadlineTime)
					chanMovingDeadline = time.After(maxGraceDeadlineTime)
					log.Info().Msg("Broadcast scheduled by config changes")
				} else {
					// If a broadcast is already scheduled, just reset the moving deadline
					chanMovingDeadline = time.After(maxGraceDeadlineTime)
				}
			} else {
				// Do nothing on non-delta updates
				continue
			}

		// A select-fallthrough doesn't exist, we are copying some code here
		case <-chanMovingDeadline:
			log.Info().Msgf("Moving deadline trigger - Broadcast envoy update")
			events.Publish(events.PubSubMessage{
				AnnouncementType: a.ProxyBroadcast,
			})
			metricsstore.DefaultMetricsStore.ProxyBroadcastEventCount.Inc()

			// broadcast done, reset timer channels
			broadcastScheduled = false
			chanMovingDeadline = make(<-chan time.Time)
			chanMaxDeadline = make(<-chan time.Time)

		case <-chanMaxDeadline:
			log.Info().Msgf("Max deadline trigger - Broadcast envoy update")
			events.Publish(events.PubSubMessage{
				AnnouncementType: a.ProxyBroadcast,
			})
			metricsstore.DefaultMetricsStore.ProxyBroadcastEventCount.Inc()

			// broadcast done, reset timer channels
			broadcastScheduled = false
			chanMovingDeadline = make(<-chan time.Time)
			chanMaxDeadline = make(<-chan time.Time)
		}
	}
}
