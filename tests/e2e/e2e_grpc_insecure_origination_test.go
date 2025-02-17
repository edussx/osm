package e2e

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/openservicemesh/osm/tests/framework"
)

const grpcbinInsecurePort = 9000

// This test originates insecure (plaintext) gRPC traffic between a client and server and
// verifies that the traffic is routable within the service mesh using HTTP routes.
// <Client app plaintext request> --> <local proxy> -- |mTLS over HTTP routes| --> <remote proxy> -- <server app plaintext request>
var _ = OSMDescribe("gRPC insecure traffic origination for client pod -> server pod usint HTTP routes",
	OSMDescribeInfo{
		Tier:   1,
		Bucket: 3,
	},
	func() {
		Context("gRPC insecure traffic origination over HTTP2 with SMI HTTP routes", func() {
			testGRPCTraffic()
		})
	})

func testGRPCTraffic() {
	const sourceName = "client"
	const destName = "server"
	var ns = []string{sourceName, destName}

	It("Tests insecure gRPC traffic origination for client pod -> server pod using HTTP routes", func() {
		// Install OSM
		Expect(Td.InstallOSM(Td.GetOSMInstallOpts())).To(Succeed())

		// Create Test NS
		for _, n := range ns {
			Expect(Td.CreateNs(n, nil)).To(Succeed())
			Expect(Td.AddNsToMesh(true, n)).To(Succeed())
		}

		// Get simple pod definitions for the gRPC server
		svcAccDef, podDef, svcDef, err := Td.SimplePodApp(
			SimplePodAppDef{
				Name:        destName,
				Namespace:   destName,
				Image:       "moul/grpcbin",
				Ports:       []int{grpcbinInsecurePort},
				AppProtocol: "grpc",
				OS:          Td.ClusterOS,
			})
		Expect(err).NotTo(HaveOccurred())

		_, err = Td.CreateServiceAccount(destName, &svcAccDef)
		Expect(err).NotTo(HaveOccurred())
		_, err = Td.CreatePod(destName, podDef)
		Expect(err).NotTo(HaveOccurred())
		dstSvc, err := Td.CreateService(destName, svcDef)
		Expect(err).NotTo(HaveOccurred())

		// Expect it to be up and running in it's receiver namespace
		Expect(Td.WaitForPodsRunningReady(destName, 90*time.Second, 1, nil)).To(Succeed())

		srcPod := setupGRPCClient(sourceName)

		trafficTargetName := "test-target" //nolint: goconst
		trafficRouteName := "routes"       //nolint: goconst

		By("Creating SMI policies")
		// Deploy allow rule client->server
		httpRG, trafficTarget := Td.CreateSimpleAllowPolicy(
			SimpleAllowPolicy{
				RouteGroupName:    trafficRouteName,
				TrafficTargetName: trafficTargetName,

				SourceNamespace:      sourceName,
				SourceSVCAccountName: sourceName,

				DestinationNamespace:      destName,
				DestinationSvcAccountName: destName,
			})

		// Configs have to be put into a monitored NS
		_, err = Td.CreateHTTPRouteGroup(sourceName, httpRG)
		Expect(err).NotTo(HaveOccurred())
		_, err = Td.CreateTrafficTarget(sourceName, trafficTarget)
		Expect(err).NotTo(HaveOccurred())

		// All ready. Expect client to reach server
		clientToServer := GRPCRequestDef{
			SourceNs:        sourceName,
			SourcePod:       srcPod.Name,
			SourceContainer: sourceName,

			Destination: fmt.Sprintf("%s.%s:%d", dstSvc.Name, dstSvc.Namespace, grpcbinInsecurePort),

			JSONRequest: "{\"greeting\": \"client\"}",
			Symbol:      "hello.HelloService/SayHello",
			UseTLS:      false, // insecure gRPC, service mesh will upgrade connection to mTLS
		}

		srcToDestStr := fmt.Sprintf("%s/%s -> %s",
			clientToServer.SourceNs, clientToServer.SourcePod, clientToServer.Destination)

		cond := Td.WaitForRepeatedSuccess(func() bool {
			result := Td.GRPCRequest(clientToServer)

			if result.Err != nil {
				Td.T.Logf("> (%s) gRPC req failed, response: %s, err: %s",
					srcToDestStr, result.Response, result.Err)
				return false
			}

			Td.T.Logf("> (%s) gRPC req succeeded, response: %s", srcToDestStr, result.Response)
			return true
		}, 5, 90*time.Second)

		Expect(cond).To(BeTrue(), "Failed testing gRPC traffic for: %s", srcToDestStr)

		By("Deleting SMI policies")
		Expect(Td.SmiClients.AccessClient.AccessV1alpha3().TrafficTargets(sourceName).Delete(context.TODO(), trafficTargetName, metav1.DeleteOptions{})).To(Succeed())
		Expect(Td.SmiClients.SpecClient.SpecsV1alpha4().HTTPRouteGroups(sourceName).Delete(context.TODO(), trafficRouteName, metav1.DeleteOptions{})).To(Succeed())

		// Expect client not to reach server
		cond = Td.WaitForRepeatedSuccess(func() bool {
			result := Td.GRPCRequest(clientToServer)

			if result.Err == nil {
				Td.T.Logf("> (%s) gRPC req did not fail, expected it to fail,  response: %s", srcToDestStr, result.Response)
				return false
			}
			Td.T.Logf("> (%s) gRPC req failed correctly, response: %s, err: %s", srcToDestStr, result.Response, result.Err)
			return true
		}, 5, 150*time.Second)
		Expect(cond).To(BeTrue())
	})
}

func setupGRPCClient(sourceName string) *corev1.Pod {
	// Get simple Pod definitions for the client
	svcAccDef, podDef, _, err := Td.SimplePodApp(SimplePodAppDef{
		Name:      sourceName,
		Namespace: sourceName,
		Command:   []string{"sleep", "365d"},
		Image:     "networld/grpcurl",
		OS:        Td.ClusterOS,
	})
	Expect(err).NotTo(HaveOccurred())

	_, err = Td.CreateServiceAccount(sourceName, &svcAccDef)
	Expect(err).NotTo(HaveOccurred())

	srcPod, err := Td.CreatePod(sourceName, podDef)
	Expect(err).NotTo(HaveOccurred())

	// Expect it to be up and running in it's receiver namespace
	Expect(Td.WaitForPodsRunningReady(sourceName, 90*time.Second, 1, nil)).To(Succeed())

	return srcPod
}
