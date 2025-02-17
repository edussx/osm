package injector

import (
	"context"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/pkg/errors"

	mapset "github.com/deckarep/golang-set"
	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	tassert "github.com/stretchr/testify/assert"
	admissionv1 "k8s.io/api/admission/v1"
	admissionregv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/openservicemesh/osm/pkg/certificate"
	"github.com/openservicemesh/osm/pkg/certificate/providers/tresor"
	"github.com/openservicemesh/osm/pkg/configurator"
	"github.com/openservicemesh/osm/pkg/constants"
	"github.com/openservicemesh/osm/pkg/k8s"
)

var _ = Describe("Test MutatingWebhookConfiguration patch", func() {
	Context("find and patches the mutating webhook and updates the CABundle", func() {
		cert := mockCertificate{}
		webhookName := "--webhookName--"
		//TODO:seed a test webhook
		testWebhookServiceNamespace := "test-namespace"
		testWebhookServiceName := "test-service-name"
		testWebhookServicePath := "/path"
		kubeClient := fake.NewSimpleClientset(&admissionregv1.MutatingWebhookConfiguration{
			ObjectMeta: metav1.ObjectMeta{
				Name: webhookName,
			},
			Webhooks: []admissionregv1.MutatingWebhook{
				{
					Name: MutatingWebhookName,
					ClientConfig: admissionregv1.WebhookClientConfig{
						Service: &admissionregv1.ServiceReference{
							Namespace: testWebhookServiceNamespace,
							Name:      testWebhookServiceName,
							Path:      &testWebhookServicePath,
						},
					},
					NamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"some-key": "some-value",
						},
					},
				},
			},
		})

		mwc := kubeClient.AdmissionregistrationV1().MutatingWebhookConfigurations()

		It("checks if the hook exists", func() {
			err := webhookExists(mwc, webhookName)
			Expect(err).ToNot(HaveOccurred())
		})

		It("checks if a non existent hook exists", func() {

			err := webhookExists(mwc, webhookName+"blah")
			Expect(err).To(HaveOccurred())
		})

		It("patches a webhook", func() {
			err := updateMutatingWebhookCABundle(cert, webhookName, kubeClient)
			Expect(err).ToNot(HaveOccurred())

		})

		It("ensures webhook is configured correctly", func() {
			webhooks, err := kubeClient.AdmissionregistrationV1().MutatingWebhookConfigurations().List(context.TODO(), metav1.ListOptions{})
			Expect(err).ToNot(HaveOccurred())
			Expect(len(webhooks.Items)).To(Equal(1))

			wh := webhooks.Items[0]
			Expect(len(wh.Webhooks)).To(Equal(1))
			Expect(wh.Webhooks[0].NamespaceSelector.MatchLabels["some-key"]).To(Equal("some-value"))
			Expect(wh.Webhooks[0].ClientConfig.Service.Namespace).To(Equal(testWebhookServiceNamespace))
			Expect(wh.Webhooks[0].ClientConfig.Service.Name).To(Equal(testWebhookServiceName))
			Expect(wh.Webhooks[0].ClientConfig.Service.Path).To(Equal(&testWebhookServicePath))
			Expect(wh.Webhooks[0].ClientConfig.CABundle).To(Equal([]byte("chain")))
		})
	})
})

type mockCertificate struct{}

func (mc mockCertificate) GetCommonName() certificate.CommonName     { return "" }
func (mc mockCertificate) GetCertificateChain() []byte               { return []byte("chain") }
func (mc mockCertificate) GetPrivateKey() []byte                     { return []byte("key") }
func (mc mockCertificate) GetIssuingCA() []byte                      { return []byte("ca") }
func (mc mockCertificate) GetExpiration() time.Time                  { return time.Now() }
func (mc mockCertificate) GetSerialNumber() certificate.SerialNumber { return "serial_number" }

func TestIsAnnotatedForInjection(t *testing.T) {
	testCases := []struct {
		name        string
		annotations map[string]string
		exists      bool
		enabled     bool
		expectError bool
	}{
		{
			name:        "annotation is set to enabled",
			annotations: map[string]string{constants.SidecarInjectionAnnotation: "enabled"},
			exists:      true,
			enabled:     true,
			expectError: false,
		},
		{
			name:        "annotation is set to yes",
			annotations: map[string]string{constants.SidecarInjectionAnnotation: "yes"},
			exists:      true,
			enabled:     true,
			expectError: false,
		},
		{
			name:        "annotation is set to true",
			annotations: map[string]string{constants.SidecarInjectionAnnotation: "true"},
			exists:      true,
			enabled:     true,
			expectError: false,
		},
		{
			name:        "annotation is set to disabled",
			annotations: map[string]string{constants.SidecarInjectionAnnotation: "disabled"},
			exists:      true,
			enabled:     false,
			expectError: false,
		},
		{
			name:        "annotation is set to no",
			annotations: map[string]string{constants.SidecarInjectionAnnotation: "no"},
			exists:      true,
			enabled:     false,
			expectError: false,
		},
		{
			name:        "annotation is set to false",
			annotations: map[string]string{constants.SidecarInjectionAnnotation: "false"},
			exists:      true,
			enabled:     false,
			expectError: false,
		},
		{
			name:        "annotation does not exist",
			annotations: map[string]string{},
			exists:      false,
			enabled:     false,
			expectError: false,
		},
		{
			name:        "annotation exists with an invalid value",
			annotations: map[string]string{constants.SidecarInjectionAnnotation: "invalid"},
			exists:      true,
			enabled:     false,
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert := tassert.New(t)

			actualExists, actualEnabled, actualErr := isAnnotatedForInjection(tc.annotations, "-kind-", "-name-")
			assert.Equal(tc.exists, actualExists)
			assert.Equal(tc.enabled, actualEnabled)
			assert.Equal(tc.expectError, actualErr != nil)
		})
	}
}

var _ = Describe("Testing mustInject, isNamespaceInjectable", func() {
	var (
		mockCtrl           *gomock.Controller
		mockKubeController *k8s.MockController
		fakeClientSet      *fake.Clientset
		wh                 *mutatingWebhook
	)

	mockCtrl = gomock.NewController(GinkgoT())
	mockKubeController = k8s.NewMockController(mockCtrl)
	fakeClientSet = fake.NewSimpleClientset()
	namespace := "test"
	osmNamespace := "osm-namespace"

	BeforeEach(func() {
		fakeClientSet = fake.NewSimpleClientset()
		wh = &mutatingWebhook{
			kubeClient:     fakeClientSet,
			kubeController: mockKubeController,
			osmNamespace:   osmNamespace,
			nonInjectNamespaces: mapset.NewSetFromSlice([]interface{}{
				metav1.NamespaceSystem,
				metav1.NamespacePublic,
				osmNamespace,
			}),
		}
	})

	It("should return true when the pod is enabled for sidecar injection", func() {
		testNamespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		retNs, err := fakeClientSet.CoreV1().Namespaces().Create(context.TODO(), testNamespace, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		podWithInjectAnnotationEnabled := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "pod-with-injection-enabled",
				Annotations: map[string]string{
					constants.SidecarInjectionAnnotation: "enabled",
				},
			},
			Spec: corev1.PodSpec{
				ServiceAccountName: "test-SA",
			},
		}
		_, err = fakeClientSet.CoreV1().Pods(namespace).Create(context.TODO(), podWithInjectAnnotationEnabled, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		mockKubeController.EXPECT().IsMonitoredNamespace(namespace).Return(true).Times(1)
		mockKubeController.EXPECT().GetNamespace(namespace).Return(retNs)

		inject, err := wh.mustInject(podWithInjectAnnotationEnabled, namespace)

		Expect(err).ToNot(HaveOccurred())
		Expect(inject).To(BeTrue())
	})

	It("should return false when the pod is disabled for sidecar injection", func() {
		testNamespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		retNs, err := fakeClientSet.CoreV1().Namespaces().Create(context.TODO(), testNamespace, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		podWithInjectAnnotationEnabled := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "pod-with-injection-disabled",
				Annotations: map[string]string{
					constants.SidecarInjectionAnnotation: "disabled",
				},
			},
			Spec: corev1.PodSpec{
				ServiceAccountName: "test-SA",
			},
		}
		_, err = fakeClientSet.CoreV1().Pods(namespace).Create(context.TODO(), podWithInjectAnnotationEnabled, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		mockKubeController.EXPECT().IsMonitoredNamespace(namespace).Return(true).Times(1)
		mockKubeController.EXPECT().GetNamespace(namespace).Return(retNs)

		inject, err := wh.mustInject(podWithInjectAnnotationEnabled, namespace)

		Expect(err).ToNot(HaveOccurred())
		Expect(inject).To(BeFalse())
	})

	It("should return true when the namespace is enabled for injection and the pod is not explicitly disabled for injection", func() {
		testNamespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
				Annotations: map[string]string{
					constants.SidecarInjectionAnnotation: "enabled",
				},
			},
		}
		retNs, err := fakeClientSet.CoreV1().Namespaces().Create(context.TODO(), testNamespace, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		podWithInjectAnnotationEnabled := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "pod-with-no-injection-annotation",
			},
			Spec: corev1.PodSpec{
				ServiceAccountName: "test-SA",
			},
		}
		_, err = fakeClientSet.CoreV1().Pods(namespace).Create(context.TODO(), podWithInjectAnnotationEnabled, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		mockKubeController.EXPECT().IsMonitoredNamespace(namespace).Return(true).Times(1)
		mockKubeController.EXPECT().GetNamespace(namespace).Return(retNs)

		inject, err := wh.mustInject(podWithInjectAnnotationEnabled, namespace)

		Expect(err).ToNot(HaveOccurred())
		Expect(inject).To(BeTrue())
	})

	It("should return false when the namespace is enabled for injection and the pod is explicitly disabled for injection", func() {
		testNamespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
				Annotations: map[string]string{
					constants.SidecarInjectionAnnotation: "enabled",
				},
			},
		}
		retNs, err := fakeClientSet.CoreV1().Namespaces().Create(context.TODO(), testNamespace, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		podWithInjectAnnotationEnabled := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "pod-with-injection-disabled",
				Annotations: map[string]string{
					constants.SidecarInjectionAnnotation: "disabled",
				},
			},
			Spec: corev1.PodSpec{
				ServiceAccountName: "test-SA",
			},
		}
		_, err = fakeClientSet.CoreV1().Pods(namespace).Create(context.TODO(), podWithInjectAnnotationEnabled, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		mockKubeController.EXPECT().IsMonitoredNamespace(namespace).Return(true).Times(1)
		mockKubeController.EXPECT().GetNamespace(namespace).Return(retNs)

		inject, err := wh.mustInject(podWithInjectAnnotationEnabled, namespace)

		Expect(err).ToNot(HaveOccurred())
		Expect(inject).To(BeFalse())
	})

	It("should return false when the pod's namespace is not being monitored", func() {
		testNamespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		_, err := fakeClientSet.CoreV1().Namespaces().Create(context.TODO(), testNamespace, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		podWithInjectAnnotationEnabled := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "pod-with-injection-enabled",
				Annotations: map[string]string{
					constants.SidecarInjectionAnnotation: "enabled",
				},
			},
			Spec: corev1.PodSpec{
				ServiceAccountName: "test-SA",
			},
		}
		_, err = fakeClientSet.CoreV1().Pods(namespace).Create(context.TODO(), podWithInjectAnnotationEnabled, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		mockKubeController.EXPECT().IsMonitoredNamespace(namespace).Return(false).Times(1)

		inject, err := wh.mustInject(podWithInjectAnnotationEnabled, namespace)

		Expect(err).ToNot(HaveOccurred())
		Expect(inject).To(BeFalse())
	})

	It("should return an error when an invalid annotation is specified", func() {
		testNamespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		_, err := fakeClientSet.CoreV1().Namespaces().Create(context.TODO(), testNamespace, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		podWithInjectAnnotationEnabled := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "pod-with-injection-enabled",
				Annotations: map[string]string{
					constants.SidecarInjectionAnnotation: "invalid-value",
				},
			},
			Spec: corev1.PodSpec{
				ServiceAccountName: "test-SA",
			},
		}
		_, err = fakeClientSet.CoreV1().Pods(namespace).Create(context.TODO(), podWithInjectAnnotationEnabled, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		mockKubeController.EXPECT().IsMonitoredNamespace(namespace).Return(true).Times(1)

		inject, err := wh.mustInject(podWithInjectAnnotationEnabled, namespace)

		Expect(err).To(HaveOccurred())
		Expect(inject).To(BeFalse())
	})

	It("Should allow a monitored app namespace", func() {
		testNamespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		_, err := fakeClientSet.CoreV1().Namespaces().Create(context.TODO(), testNamespace, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		mockKubeController.EXPECT().IsMonitoredNamespace(testNamespace.Name).Return(true).Times(1)
		allowed := wh.isNamespaceInjectable(testNamespace.Name)
		Expect(allowed).To(BeTrue())
	})

	It("Should not allow an osm-controller's namespace", func() {
		allowed := wh.isNamespaceInjectable(osmNamespace)
		Expect(allowed).To(BeFalse())
	})

	It("Should not allow an kubernetes system namespace", func() {
		allowed := wh.isNamespaceInjectable(metav1.NamespaceSystem)
		Expect(allowed).To(BeFalse())
	})

	It("Should not allow an kubernetes public namespace", func() {
		allowed := wh.isNamespaceInjectable(metav1.NamespacePublic)
		Expect(allowed).To(BeFalse())
	})

	It("Should not allow an unmonitored app namespace", func() {
		testNamespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		_, err := fakeClientSet.CoreV1().Namespaces().Create(context.TODO(), testNamespace, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		mockKubeController.EXPECT().IsMonitoredNamespace(testNamespace.Name).Return(false).Times(1)
		allowed := wh.isNamespaceInjectable(testNamespace.Name)
		Expect(allowed).To(BeFalse())
	})
})

var _ = Describe("Testing Injector Functions", func() {
	admissionRequestBody := `{
  "kind": "AdmissionReview",
  "apiVersion": "admission.k8s.io/v1",
  "request": {
    "uid": "11111111-2222-3333-4444-555555555555",
    "kind": {
      "group": "",
      "version": "v1",
      "kind": "PodExecOptions"
    },
    "resource": {
      "group": "",
      "version": "v1",
      "resource": "pods"
    },
    "subResource": "exec",
    "requestKind": {
      "group": "",
      "version": "v1",
      "kind": "PodExecOptions"
    },
    "requestResource": {
      "group": "",
      "version": "v1",
      "resource": "pods"
    },
    "requestSubResource": "exec",
    "name": "some-pod-1111111111-22222",
    "namespace": "default",
    "operation": "CONNECT",
    "userInfo": {
      "username": "user",
      "groups": []
    },
    "object": {
      "kind": "PodExecOptions",
      "apiVersion": "v1",
      "stdin": true,
      "stdout": true,
      "tty": true,
      "container": "some-pod",
      "command": ["bin/bash"]
    },
    "oldObject": null,
    "dryRun": false,
    "options": null
  }
}`
	It("creates new webhook", func() {
		injectorConfig := Config{}
		kubeClient := fake.NewSimpleClientset()
		var kubeController k8s.Controller
		meshName := "-mesh-name-"
		osmNamespace := "-osm-namespace-"
		webhookName := "-webhook-name-"
		stop := make(<-chan struct{})
		mockController := gomock.NewController(GinkgoT())
		cfg := configurator.NewMockConfigurator(mockController)
		certManager := tresor.NewFakeCertManager(cfg)

		cfg.EXPECT().GetCertKeyBitSize().Return(2048).AnyTimes()

		actualErr := NewMutatingWebhook(injectorConfig, kubeClient, certManager, kubeController, meshName, osmNamespace, webhookName, stop, cfg)
		expectedErrorMessage := "Error configuring MutatingWebhookConfiguration -webhook-name-: mutatingwebhookconfigurations.admissionregistration.k8s.io \"-webhook-name-\" not found"
		Expect(actualErr.Error()).To(Equal(expectedErrorMessage))
	})

	It("creates new webhook", func() {
		client := fake.NewSimpleClientset()
		mockNsController := k8s.NewMockController(gomock.NewController(GinkgoT()))
		mockNsController.EXPECT().GetNamespace("default").Return(&corev1.Namespace{})
		testNamespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "default",
			},
		}
		_, err := client.CoreV1().Namespaces().Create(context.TODO(), testNamespace, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
		wh := &mutatingWebhook{
			kubeClient:          client,
			kubeController:      mockNsController,
			nonInjectNamespaces: mapset.NewSet(),
		}

		req := httptest.NewRequest("GET", "/a/b/c", strings.NewReader(admissionRequestBody))
		req.Header = map[string][]string{
			"Content-Type": {"application/json"},
		}
		w := httptest.NewRecorder()
		mockNsController.EXPECT().IsMonitoredNamespace("default").Return(true).Times(1)
		wh.podCreationHandler(w, req)

		resp := w.Result()
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		expected := "{\"kind\":\"AdmissionReview\",\"apiVersion\":\"admission.k8s.io/v1\",\"response\":{\"uid\":\"11111111-2222-3333-4444-555555555555\",\"allowed\":true}}"
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		log.Debug().Msgf("Actual: %s", string(bodyBytes))
		Expect(string(bodyBytes)).To(Equal(expected))
	})

	It("getAdmissionReqResp creates admission ", func() {
		namespace := "default"
		client := fake.NewSimpleClientset()
		mockKubeController := k8s.NewMockController(gomock.NewController(GinkgoT()))
		mockKubeController.EXPECT().GetNamespace(namespace).Return(&corev1.Namespace{})
		mockKubeController.EXPECT().IsMonitoredNamespace(namespace).Return(true).Times(1)

		wh := &mutatingWebhook{
			kubeClient:          client,
			kubeController:      mockKubeController,
			nonInjectNamespaces: mapset.NewSet(),
		}
		proxyUUID := uuid.New()

		// !! ACTION !!
		requestForNamespace, admissionResp := wh.getAdmissionReqResp(proxyUUID, []byte(admissionRequestBody))

		Expect(requestForNamespace).To(Equal("default"))

		expectedAdmissionResponse := admissionv1.AdmissionReview{
			TypeMeta: metav1.TypeMeta{Kind: "AdmissionReview", APIVersion: "admission.k8s.io/v1"},
			Request:  nil,
			Response: &admissionv1.AdmissionResponse{
				UID:              "11111111-2222-3333-4444-555555555555",
				Allowed:          true,
				Result:           nil,
				Patch:            nil,
				PatchType:        nil,
				AuditAnnotations: nil,
			},
		}
		Expect(admissionResp).To(Equal(expectedAdmissionResponse))
	})

	It("handles health requests", func() {
		mockNsController := k8s.NewMockController(gomock.NewController(GinkgoT()))
		mockNsController.EXPECT().GetNamespace("default").Return(&corev1.Namespace{})
		w := httptest.NewRecorder()
		body := strings.NewReader(``)
		req := httptest.NewRequest("GET", "/a/b/c", body)

		// Action !!
		healthHandler(w, req)

		resp := w.Result()
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		expected := "Health OK"
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		Expect(string(bodyBytes)).To(Equal(expected))
	})

	It("mutate() handles nil admission request", func() {
		client := fake.NewSimpleClientset()
		mockNsController := k8s.NewMockController(gomock.NewController(GinkgoT()))
		mockNsController.EXPECT().GetNamespace("default").Return(&corev1.Namespace{})
		wh := &mutatingWebhook{
			kubeClient:          client,
			kubeController:      mockNsController,
			nonInjectNamespaces: mapset.NewSet(),
		}
		proxyUUID := uuid.New()

		// Action !!
		actual := wh.mutate(nil, proxyUUID)

		expected := admissionv1.AdmissionResponse{
			Result: &metav1.Status{
				Message: "nil admission request",
			},
		}
		Expect(actual).To(Equal(&expected))
	})

	It("patches admission response", func() {
		admRes := admissionv1.AdmissionResponse{
			Patch: []byte(""),
		}
		patchBytes := []byte("abc")
		patchAdmissionResponse(&admRes, patchBytes)

		expectedPatchType := admissionv1.PatchTypeJSONPatch
		expected := admissionv1.AdmissionResponse{
			Patch:     []byte("abc"),
			PatchType: &expectedPatchType,
		}
		Expect(admRes).To(Equal(expected))
	})

	It("creates partial mutating webhook configuration", func() {
		cert := mockCertificate{}
		webhookConfigName := "-webhook-config-name-"

		actual := getPartialMutatingWebhookConfiguration(cert, webhookConfigName)

		expected := admissionregv1.MutatingWebhookConfiguration{
			ObjectMeta: metav1.ObjectMeta{
				Name: "-webhook-config-name-",
			},
			Webhooks: []admissionregv1.MutatingWebhook{
				{
					Name: MutatingWebhookName,
					ClientConfig: admissionregv1.WebhookClientConfig{
						CABundle: cert.GetCertificateChain(),
					},
					SideEffects: func() *admissionregv1.SideEffectClass {
						sideEffect := admissionregv1.SideEffectClassNoneOnDryRun
						return &sideEffect
					}(),
					AdmissionReviewVersions: []string{"v1"},
				},
			},
		}
		Expect(actual).To(Equal(expected))
	})
})

func TestIsAnnotatedForPortExclusion(t *testing.T) {
	testCases := []struct {
		name          string
		annotations   map[string]string
		forAnnotation string
		expectedError error
		expectedPorts []int
	}{
		{
			name:          "contains outbound port exclusion list annotation",
			annotations:   map[string]string{outboundPortExclusionListAnnotation: "6060, 7070"},
			forAnnotation: outboundPortExclusionListAnnotation,
			expectedError: nil,
			expectedPorts: []int{6060, 7070},
		},
		{
			name:          "contains inbound port exclusion list annotation",
			annotations:   map[string]string{inboundPortExclusionListAnnotation: "6060, 7070"},
			forAnnotation: inboundPortExclusionListAnnotation,
			expectedError: nil,
			expectedPorts: []int{6060, 7070},
		},
		{
			name:          "does not contains port exclusion list annontation",
			annotations:   nil,
			forAnnotation: "",
			expectedError: nil,
			expectedPorts: nil,
		},
		{
			name:          "contains outbound port exclusion list annontation but invalid port",
			annotations:   map[string]string{outboundPortExclusionListAnnotation: "6060, -7070"},
			forAnnotation: outboundPortExclusionListAnnotation,
			expectedError: errors.Errorf("Invalid port '%s' specified for annotation '%s'", "-7070", outboundPortExclusionListAnnotation),
			expectedPorts: nil,
		},
		{
			name:          "contains inbound port exclusion list annontation but invalid port",
			annotations:   map[string]string{inboundPortExclusionListAnnotation: "6060, -7070"},
			forAnnotation: inboundPortExclusionListAnnotation,
			expectedError: errors.Errorf("Invalid port '%s' specified for annotation '%s'", "-7070", inboundPortExclusionListAnnotation),
			expectedPorts: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert := tassert.New(t)

			ports, err := isAnnotatedForPortExclusion(tc.annotations, tc.forAnnotation, "-kind-", "-name-")
			if err != nil {
				assert.EqualError(tc.expectedError, err.Error())
			} else {
				assert.Equal(tc.expectedError, err)
			}
			assert.ElementsMatch(tc.expectedPorts, ports)
		})
	}
}

func TestGetPodOutboundPortExclusionList(t *testing.T) {
	testCases := []struct {
		name          string
		podAnnotation map[string]string
		forAnnotation string
		expectedError error
		expectedPorts []int
	}{
		{
			name:          "contains outbound port exclusion list annotation",
			podAnnotation: map[string]string{outboundPortExclusionListAnnotation: "6060, 7070"},
			forAnnotation: outboundPortExclusionListAnnotation,
			expectedError: nil,
			expectedPorts: []int{6060, 7070},
		},
		{
			name:          "contains inbound port exclusion list annotation",
			podAnnotation: map[string]string{inboundPortExclusionListAnnotation: "6060, 7070"},
			forAnnotation: inboundPortExclusionListAnnotation,
			expectedError: nil,
			expectedPorts: []int{6060, 7070},
		},
		{
			name:          "does not contains any port exclusion list annontation",
			podAnnotation: nil,
			forAnnotation: "",
			expectedError: nil,
			expectedPorts: nil,
		},
		{
			name:          "contains outbound port exclusion list annontation but invalid port",
			podAnnotation: map[string]string{outboundPortExclusionListAnnotation: "6060, -7070"},
			forAnnotation: outboundPortExclusionListAnnotation,
			expectedError: errors.Errorf("Invalid port '%s' specified for annotation '%s'", "-7070", outboundPortExclusionListAnnotation),
			expectedPorts: nil,
		},
		{
			name:          "contains inbound port exclusion list annontation but invalid port",
			podAnnotation: map[string]string{inboundPortExclusionListAnnotation: "6060, -7070"},
			forAnnotation: inboundPortExclusionListAnnotation,
			expectedError: errors.Errorf("Invalid port '%s' specified for annotation '%s'", "-7070", inboundPortExclusionListAnnotation),
			expectedPorts: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert := tassert.New(t)

			mockCtrl := gomock.NewController(GinkgoT())
			mockKubeController := k8s.NewMockController(mockCtrl)
			fakeClientSet := fake.NewSimpleClientset()
			namespace := "test"
			osmNamespace := "osm-namespace"

			wh := &mutatingWebhook{
				kubeClient:     fakeClientSet,
				kubeController: mockKubeController,
				osmNamespace:   osmNamespace,
				nonInjectNamespaces: mapset.NewSetFromSlice([]interface{}{
					metav1.NamespaceSystem,
					metav1.NamespacePublic,
					osmNamespace,
				}),
			}

			testNamespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespace,
				},
			}
			retNs, _ := fakeClientSet.CoreV1().Namespaces().Create(context.TODO(), testNamespace, metav1.CreateOptions{})

			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "pod-test",
					Annotations: tc.podAnnotation,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: "test-SA",
				},
			}

			_, err := fakeClientSet.CoreV1().Pods(namespace).Create(context.TODO(), pod, metav1.CreateOptions{})
			Expect(err).ToNot(HaveOccurred())

			mockKubeController.EXPECT().IsMonitoredNamespace(namespace).Return(true).Times(1)
			mockKubeController.EXPECT().GetNamespace(namespace).Return(retNs)

			ports, err := wh.getPortExclusionListForPod(pod, namespace, tc.forAnnotation)
			if err != nil {
				assert.EqualError(tc.expectedError, err.Error())
			} else {
				assert.Equal(tc.expectedError, err)
			}
			assert.ElementsMatch(tc.expectedPorts, ports)
		})
	}
}
