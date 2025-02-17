package registry

import (
	v1 "k8s.io/api/core/v1"

	"github.com/openservicemesh/osm/pkg/announcements"
	"github.com/openservicemesh/osm/pkg/certificate"
	"github.com/openservicemesh/osm/pkg/k8s/events"
)

// ReleaseCertificateHandler releases certificates based on podDelete events
// returns a stop channel which can be used to stop the inner handler
func (pr *ProxyRegistry) ReleaseCertificateHandler(certManager certificate.Manager) chan struct{} {
	podDeleteSubscription := events.Subscribe(announcements.PodDeleted)
	stop := make(chan struct{})

	go func() {
		for {
			select {
			case <-stop:
				return
			case podDeletedMsg := <-podDeleteSubscription:
				psubMessage, castOk := podDeletedMsg.(events.PubSubMessage)
				if !castOk {
					log.Error().Msgf("Error casting PubSubMessage: %v", psubMessage)
					continue
				}

				// guaranteed can only be a PodDeleted event
				deletedPodObj, castOk := psubMessage.OldObj.(*v1.Pod)
				if !castOk {
					log.Error().Msgf("Failed to cast to *v1.Pod: %v", psubMessage.OldObj)
					continue
				}

				podUID := deletedPodObj.GetObjectMeta().GetUID()
				if podIface, ok := pr.podUIDToCN.Load(podUID); ok {
					endpointCN := podIface.(certificate.CommonName)
					log.Warn().Msgf("Pod with UID %s found in Mesh Catalog; Releasing certificate %s", podUID, endpointCN)
					certManager.ReleaseCertificate(endpointCN)

					// Request a broadcast update, just for security.
					// Dispatcher code also handles PodDelete, so probably the two will get coalesced.
					events.Publish(events.PubSubMessage{
						AnnouncementType: announcements.ScheduleProxyBroadcast,
						NewObj:           nil,
						OldObj:           nil,
					})
				} else {
					log.Warn().Msgf("Pod with UID %s not found in Mesh Catalog", podUID)
				}
			}
		}
	}()

	return stop
}
