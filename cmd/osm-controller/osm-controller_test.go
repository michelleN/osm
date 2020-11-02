package main

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/pkg/errors"

	"github.com/stretchr/testify/assert"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	testclient "k8s.io/client-go/kubernetes/fake"

	"github.com/openservicemesh/osm/pkg/certificate"
	"github.com/openservicemesh/osm/pkg/certificate/providers/tresor"
	"github.com/openservicemesh/osm/pkg/configurator"
	"github.com/openservicemesh/osm/pkg/constants"
	"github.com/openservicemesh/osm/pkg/debugger"
)

type FakeDebugServer struct {
	stopCount int
	stopErr   error
}

func (f *FakeDebugServer) Stop() error {
	f.stopCount++
	if f.stopErr != nil {
		return errors.Errorf("Debug server error")
	}
	return nil
}

func (f *FakeDebugServer) Start() {
}

func TestConfigureDebugServer(t *testing.T) {
	assert := assert.New(t)
	const validRoutePath = "/debug/test1"

	mockCtrl := gomock.NewController(t)
	mockDebugConfig := debugger.NewMockDebugServer(mockCtrl)
	mockDebugConfig.EXPECT().GetHandlers().Return(map[string]http.Handler{
		validRoutePath: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
	}).AnyTimes()
	kubeClient := testclient.NewSimpleClientset()
	stop := make(chan struct{})
	osmNamespace := "-test-osm-namespace-"
	osmConfigMapName := "-test-osm-config-map-"

	cfg := configurator.NewConfigurator(kubeClient, stop, osmNamespace, osmConfigMapName)
	errCh := make(chan error, 1)

	fakeDebugServer := FakeDebugServer{0, nil}
	fakeDebugServerGetErr := FakeDebugServer{0, errors.Errorf("Debug server error")}

	testCases := []struct {
		name                       string
		initialDebugServerEnabled  bool
		changeDebugServerEnabledTo bool
		c                          controller
		expectedStopCount          int
		expectedStopErr            bool
		expectedDebugServerRunning bool
	}{
		{
			name:                       "debug server is already on, do nothing",
			initialDebugServerEnabled:  true,
			changeDebugServerEnabledTo: true,
			c:                          controller{debugServerRunning: true, debugComponents: mockDebugConfig, debugServer: nil},
			expectedStopCount:          0,
			expectedStopErr:            false,
			expectedDebugServerRunning: true,
		},
		{
			name:                       "debug server is already off, do nothing",
			initialDebugServerEnabled:  false,
			changeDebugServerEnabledTo: false,
			c:                          controller{debugServerRunning: false, debugComponents: mockDebugConfig, debugServer: nil},
			expectedStopCount:          0,
			expectedStopErr:            false,
			expectedDebugServerRunning: false,
		},
		{
			name:                       "turn on debug server",
			initialDebugServerEnabled:  false,
			changeDebugServerEnabledTo: true,
			c:                          controller{debugServerRunning: false, debugComponents: mockDebugConfig, debugServer: nil},
			expectedStopCount:          0,
			expectedStopErr:            false,
			expectedDebugServerRunning: true,
		},
		{
			name:                       "turn off debug server",
			initialDebugServerEnabled:  true,
			changeDebugServerEnabledTo: false,
			c:                          controller{debugServerRunning: true, debugComponents: mockDebugConfig, debugServer: &fakeDebugServer},
			expectedStopCount:          1,
			expectedStopErr:            false,
			expectedDebugServerRunning: false,
		},
		{
			name:                       "error when turning off debug server",
			initialDebugServerEnabled:  true,
			changeDebugServerEnabledTo: false,
			c:                          controller{debugServerRunning: true, debugComponents: mockDebugConfig, debugServer: &fakeDebugServerGetErr},
			expectedStopCount:          1,
			expectedStopErr:            true,
			expectedDebugServerRunning: false,
		},
	}
	firstTest := true

	for _, tests := range testCases {
		t.Run(fmt.Sprintf("Test: %s", tests.name), func(t *testing.T) {
			defaultConfigMap := map[string]string{
				"enabled_debug_server": strconv.FormatBool(tests.initialDebugServerEnabled),
			}
			configMap := v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: osmNamespace,
					Name:      osmConfigMapName,
				},
				Data: defaultConfigMap,
			}
			if firstTest {
				firstTest = false
				_, err := kubeClient.CoreV1().ConfigMaps(osmNamespace).Create(context.TODO(), &configMap, metav1.CreateOptions{})
				assert.Nil(err)
			}

			defaultConfigMap["enable_debug_server"] = strconv.FormatBool(tests.changeDebugServerEnabledTo)
			configMap.Data = defaultConfigMap

			go tests.c.configureDebugServer(cfg, errCh)
			_, err := kubeClient.CoreV1().ConfigMaps(osmNamespace).Update(context.TODO(), &configMap, metav1.UpdateOptions{})
			assert.Nil(err)
			time.Sleep(time.Second)
			close(stop)

			assert.Equal(tests.expectedDebugServerRunning, tests.c.debugServerRunning)

			if !tests.initialDebugServerEnabled && tests.changeDebugServerEnabledTo {
				assert.NotNil(tests.c.debugServer)
			} else {
				assert.Nil(tests.c.debugServer)
			}

			if tests.expectedStopErr {
				assert.Equal(tests.expectedStopCount, fakeDebugServerGetErr.stopCount)
				assert.NotEmpty(errCh)
				err := <-errCh
				assert.NotNil(err)
			} else {
				assert.Equal(tests.expectedStopCount, fakeDebugServer.stopCount)
			}

			stop = make(chan struct{})
			cfg = configurator.NewConfigurator(kubeClient, stop, osmNamespace, osmConfigMapName)
		})
	}
}

func TestCreateCABundleKubernetesSecret(t *testing.T) {
	assert := assert.New(t)

	cache := make(map[certificate.CommonName]certificate.Certificater)
	certManager := tresor.NewFakeCertManager(&cache, nil)
	testName := "--secret--name--"
	namespace := "--namespace--"
	k8sClient := testclient.NewSimpleClientset()

	err := createOrUpdateCABundleKubernetesSecret(k8sClient, certManager, namespace, testName)
	assert.Nil(err)

	actual, err := k8sClient.CoreV1().Secrets(namespace).Get(context.Background(), testName, metav1.GetOptions{})
	assert.Nil(err)

	expected := "-----BEGIN CERTIFICATE-----\nMIID"
	stringPEM := string(actual.Data[constants.KubernetesOpaqueSecretCAKey])[:len(expected)]
	assert.Equal(stringPEM, expected)

	expectedRootCert, err := certManager.GetRootCertificate()
	assert.Nil(err)
	assert.Equal(actual.Data[constants.KubernetesOpaqueSecretCAKey], expectedRootCert.GetCertificateChain())
}

func TestJoinURL(t *testing.T) {
	assert := assert.New(t)
	type joinURLtest struct {
		baseURL        string
		path           string
		expectedOutput string
	}
	joinURLtests := []joinURLtest{
		{"http://foo", "/bar", "http://foo/bar"},
		{"http://foo/", "/bar", "http://foo/bar"},
		{"http://foo/", "bar", "http://foo/bar"},
		{"http://foo", "bar", "http://foo/bar"},
	}

	for _, ju := range joinURLtests {
		result := joinURL(ju.baseURL, ju.path)
		assert.Equal(result, ju.expectedOutput)
	}
}
