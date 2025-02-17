package registry

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	configFake "github.com/openservicemesh/osm/pkg/gen/client/config/clientset/versioned/fake"

	"github.com/openservicemesh/osm/pkg/announcements"
	"github.com/openservicemesh/osm/pkg/certificate"
	"github.com/openservicemesh/osm/pkg/certificate/providers/tresor"
	"github.com/openservicemesh/osm/pkg/configurator"
	"github.com/openservicemesh/osm/pkg/envoy"
	"github.com/openservicemesh/osm/pkg/k8s/events"
)

var _ = Describe("Test Announcement Handlers", func() {
	var proxyRegistry *ProxyRegistry
	var podUID string
	var proxy *envoy.Proxy
	var certManager certificate.Manager
	envoyCN := certificate.CommonName(fmt.Sprintf("%s.sidecar.foo.bar", uuid.New()))

	BeforeEach(func() {
		proxyRegistry = NewProxyRegistry(nil)
		podUID = uuid.New().String()

		stop := make(<-chan struct{})
		configClient := configFake.NewSimpleClientset()

		osmNamespace := "-test-osm-namespace-"
		osmMeshConfigName := "-test-osm-mesh-config-"
		cfg := configurator.NewConfigurator(configClient, stop, osmNamespace, osmMeshConfigName)
		certManager = tresor.NewFakeCertManager(cfg)

		_, err := certManager.IssueCertificate(envoyCN, 5*time.Second)
		Expect(err).ToNot(HaveOccurred())

		proxy, err = envoy.NewProxy(envoyCN, "-cert-serial-number-", nil)
		Expect(err).ToNot(HaveOccurred())

		proxy.PodMetadata = &envoy.PodMetadata{
			UID: podUID,
		}

		proxyRegistry.RegisterProxy(proxy)
	})

	Context("test releaseCertificate()", func() {
		var stopChannel chan struct{}
		BeforeEach(func() {
			stopChannel = proxyRegistry.ReleaseCertificateHandler(certManager)
		})

		AfterEach(func() {
			stopChannel <- struct{}{}
		})

		It("deletes certificate when Pod is terminated", func() {
			// Ensure setup is correct
			{
				certs, err := certManager.ListCertificates()
				Expect(err).ToNot(HaveOccurred())
				Expect(len(certs)).To(Equal(1))
			}

			// Register to Update proxies event. We should see a schedule broadcast update
			// requested by the handler when the certificate is released.
			rcvBroadcastChannel := events.Subscribe(announcements.ScheduleProxyBroadcast)

			// Publish a podDeleted event
			events.Publish(events.PubSubMessage{
				AnnouncementType: announcements.PodDeleted,
				NewObj:           nil,
				OldObj: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						UID: types.UID(podUID),
					},
				},
			})

			// Expect the certificate to eventually be gone for the deleted Pod
			Eventually(func() int {
				certs, err := certManager.ListCertificates()
				Expect(err).ToNot(HaveOccurred())
				return len(certs)
			}).Should(Equal(0))

			select {
			case <-rcvBroadcastChannel:
				// broadcast event received
			case <-time.After(1 * time.Second):
				Fail("Did not see a broadcast request in time")
			}
		})

		It("ignores events other than pod-deleted", func() {
			var connectedProxies []envoy.Proxy
			proxyRegistry.connectedProxies.Range(func(key interface{}, value interface{}) bool {
				connectedProxy := value.(connectedProxy)
				connectedProxies = append(connectedProxies, *connectedProxy.proxy)
				return true // continue the iteration
			})

			Expect(len(connectedProxies)).To(Equal(1))
			Expect(connectedProxies[0]).To(Equal(*proxy))

			// Publish some event unrelated to podDeleted
			events.Publish(events.PubSubMessage{
				AnnouncementType: announcements.IngressAdded,
				NewObj:           nil,
				OldObj: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						UID: types.UID(proxy.PodMetadata.UID),
					},
				},
			})

			// Give some grace period for event to propagate
			time.Sleep(500 * time.Millisecond)

			// Ensure it was not deleted due to an unrelated event
			certs, err := certManager.ListCertificates()
			Expect(err).ToNot(HaveOccurred())
			Expect(len(certs)).To(Equal(1))
		})
	})
})
