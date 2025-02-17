package providers

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/google/uuid"
	tassert "github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/openservicemesh/osm/pkg/certificate"
	"github.com/openservicemesh/osm/pkg/certificate/providers/tresor"
	"github.com/openservicemesh/osm/pkg/configurator"
	"github.com/openservicemesh/osm/pkg/constants"
	"github.com/openservicemesh/osm/pkg/tests"
)

func TestGetCertificateManager(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	mockConfigurator := configurator.NewMockConfigurator(mockCtrl)

	mockConfigurator.EXPECT().IsDebugServerEnabled().Return(false).AnyTimes()
	mockConfigurator.EXPECT().GetCertKeyBitSize().Return(2048).AnyTimes()
	mockConfigurator.EXPECT().GetServiceCertValidityPeriod().Return(1 * time.Hour).AnyTimes()

	testCases := []struct {
		name string
		util *Config

		expectError bool
	}{
		{
			name: "tresor as the certificate manager",
			util: &Config{
				caBundleSecretName: "osm-ca-bundle",
				providerKind:       TresorKind,
				providerNamespace:  "osm-system",
				cfg:                mockConfigurator,
				kubeClient:         fake.NewSimpleClientset(),
			},
			expectError: false,
		},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("Testing test case %d: %s", i, tc.name), func(t *testing.T) {
			assert := tassert.New(t)

			manager, _, err := tc.util.GetCertificateManager()
			assert.NotNil(manager)
			assert.Equal(tc.expectError, err != nil)

			switch tc.util.providerKind {
			case TresorKind:
				_, err = tc.util.kubeClient.CoreV1().Secrets(tc.util.providerNamespace).Get(context.TODO(), tc.util.caBundleSecretName, metav1.GetOptions{})
				assert.NoError(err)
			default:
				assert.Fail("Unknown provider kind")
			}
		})
	}
}

func TestSynchronizeCertificate(t *testing.T) {
	assert := tassert.New(t)
	kubeClient := fake.NewSimpleClientset()

	// Create some cert, using tresor's api for simplicity
	cert, err := tresor.NewCA("common-name", time.Hour, "test-country", "test-locality", "test-org")
	assert.NoError(err)

	wg := sync.WaitGroup{}
	wg.Add(10)
	certResults := make([]certificate.Certificater, 10)

	// Test synchronization, expect all routines end up with the same cert
	for i := 0; i < 10; i++ {
		go func(num int) {
			defer wg.Done()

			resCert, err := GetCertificateFromSecret("test", "test", cert, kubeClient)
			assert.NoError(err)

			certResults[num] = resCert
		}(i)
	}
	wg.Wait()

	// Verifiy all of them loaded the exact same cert
	for i := 0; i < 9; i++ {
		assert.Equal(certResults[i], certResults[i+1])
	}
}

func TestGetCertificateFromKubernetes(t *testing.T) {
	assert := tassert.New(t)

	certPEM, err := tests.GetPEMCert()
	assert.NoError(err)
	keyPEM, err := tests.GetPEMPrivateKey()
	assert.NoError(err)

	ns := uuid.New().String()
	secretName := uuid.New().String()

	testCases := []struct {
		secret       *corev1.Secret
		expectError  bool
		expectNilVal bool
	}{
		{
			// Valid cert, valid test
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: ns,
				},
				Data: map[string][]byte{
					constants.KubernetesOpaqueSecretCAKey:             certPEM,
					constants.KubernetesOpaqueSecretCAExpiration:      []byte("2020-05-07T14:25:18.677Z"),
					constants.KubernetesOpaqueSecretRootPrivateKeyKey: keyPEM,
				},
			},
			expectError:  false,
			expectNilVal: false,
		},
		{
			// Error when cert fetch is not present
			secret:       nil,
			expectError:  true,
			expectNilVal: true,
		},
		{
			// Error when CA key is missing
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: ns,
				},
				Data: map[string][]byte{
					constants.KubernetesOpaqueSecretCAExpiration:      []byte("2020-05-07T14:25:18.677Z"),
					constants.KubernetesOpaqueSecretRootPrivateKeyKey: keyPEM,
				},
			},
			expectError:  true,
			expectNilVal: true,
		},
		{
			// Error when Private Key is missing
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: ns,
				},
				Data: map[string][]byte{
					constants.KubernetesOpaqueSecretCAKey:        certPEM,
					constants.KubernetesOpaqueSecretCAExpiration: []byte("2020-05-07T14:25:18.677Z"),
				},
			},
			expectError:  true,
			expectNilVal: true,
		},
		{
			// Error when Expiration is missing
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: ns,
				},
				Data: map[string][]byte{
					constants.KubernetesOpaqueSecretCAKey:             certPEM,
					constants.KubernetesOpaqueSecretRootPrivateKeyKey: keyPEM,
				},
			},
			expectError:  true,
			expectNilVal: true,
		},
		{
			// Error when Parsing expiration date
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: ns,
				},
				Data: map[string][]byte{
					constants.KubernetesOpaqueSecretCAKey:             certPEM,
					constants.KubernetesOpaqueSecretCAExpiration:      []byte("Invalid expiration date"),
					constants.KubernetesOpaqueSecretRootPrivateKeyKey: keyPEM,
				},
			},
			expectError:  true,
			expectNilVal: true,
		},
	}

	for _, testElement := range testCases {
		kubeClient := fake.NewSimpleClientset()

		if testElement.secret != nil {
			_, err = kubeClient.CoreV1().Secrets(ns).Create(context.Background(), testElement.secret, metav1.CreateOptions{})
			assert.NoError(err)
		}

		cert, err := GetCertFromKubernetes(ns, secretName, kubeClient)

		assert.Equal(testElement.expectError, err != nil)
		assert.Equal(testElement.expectNilVal, cert == nil)
	}
}

func TestValidateCertManagerOptions(t *testing.T) {
	assert := tassert.New(t)

	testCases := []struct {
		testName  string
		options   CertManagerOptions
		expectErr bool
	}{
		{
			testName: "Empty issuer",
			options: CertManagerOptions{
				IssuerName:  "",
				IssuerKind:  "test-kind",
				IssuerGroup: "test-group",
			},
			expectErr: true,
		},
		{
			testName: "Empty kind",
			options: CertManagerOptions{
				IssuerName:  "test-name",
				IssuerKind:  "",
				IssuerGroup: "test-group",
			},
			expectErr: true,
		},
		{
			testName: "Empty group",
			options: CertManagerOptions{
				IssuerName:  "test-name",
				IssuerKind:  "test-kind",
				IssuerGroup: "",
			},
			expectErr: true,
		},
		{
			testName: "Valid cert manager opts",
			options: CertManagerOptions{
				IssuerName:  "test-name",
				IssuerKind:  "test-kind",
				IssuerGroup: "test-group",
			},
			expectErr: false,
		},
	}

	for _, t := range testCases {
		err := ValidateCertManagerOptions(t.options)
		if t.expectErr {
			assert.Error(err, "test '%s' didn't error as expected", t.testName)
		} else {
			assert.NoError(err, "test '%s' didn't succeed as expected", t.testName)
		}
	}
}

func TestValidateVaultOptions(t *testing.T) {
	assert := tassert.New(t)

	testCases := []struct {
		testName  string
		options   VaultOptions
		expectErr bool
	}{
		{
			testName: "invalid proto",
			options: VaultOptions{
				VaultProtocol: "ftp",
				VaultHost:     "vault-host",
				VaultToken:    "vault-token",
				VaultRole:     "vault-role",
			},
			expectErr: true,
		},
		{
			testName: "Empty host",
			options: VaultOptions{
				VaultProtocol: "http",
				VaultHost:     "",
				VaultToken:    "vault-token",
				VaultRole:     "vault-role",
			},
			expectErr: true,
		},
		{
			testName: "Empty token",
			options: VaultOptions{
				VaultProtocol: "https",
				VaultHost:     "vault-host",
				VaultToken:    "",
				VaultRole:     "vault-role",
			},
			expectErr: true,
		},
		{
			testName: "Empty role",
			options: VaultOptions{
				VaultProtocol: "http",
				VaultHost:     "vault-host",
				VaultToken:    "vault-token",
				VaultRole:     "",
			},
			expectErr: true,
		},
		{
			testName: "Empty role",
			options: VaultOptions{
				VaultProtocol: "https",
				VaultHost:     "vault-host",
				VaultToken:    "vault-token",
				VaultRole:     "",
			},
			expectErr: true,
		},
		{
			testName: "Valid config",
			options: VaultOptions{
				VaultProtocol: "https",
				VaultHost:     "vault-host",
				VaultToken:    "vault-token",
				VaultRole:     "role",
			},
			expectErr: false,
		},
	}

	for _, t := range testCases {
		err := ValidateVaultOptions(t.options)
		if t.expectErr {
			assert.Error(err, "test '%s' didn't error as expected", t.testName)
		} else {
			assert.NoError(err, "test '%s' didn't succeed as expected", t.testName)
		}
	}
}
