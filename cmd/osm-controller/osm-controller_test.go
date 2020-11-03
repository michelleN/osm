package main

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/pkg/errors"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	testclient "k8s.io/client-go/kubernetes/fake"

	"github.com/openservicemesh/osm/pkg/configurator"
	"github.com/openservicemesh/osm/pkg/debugger"
)

const (
	validRoutePath       = "/debug/test1"
	testOSMNamespace     = "-test-osm-namespace-"
	testOSMConfigMapName = "-test-osm-config-map-"
)

type FakeDebugServer struct {
	stopCount int

	stopErr error
	wg      sync.WaitGroup // create a wait group, this will allow you to block later

}

func (f *FakeDebugServer) Stop() error {
	f.stopCount++
	if f.stopErr != nil {
		f.wg.Done()
		return errors.Errorf("Debug server error")
	}
	f.wg.Done()
	return nil
}

func (f *FakeDebugServer) Start() {
	f.wg.Done()
}

func mockDebugConfig(mockCtrl *gomock.Controller) *debugger.MockDebugServer {
	mockDebugConfig := debugger.NewMockDebugServer(mockCtrl)
	mockDebugConfig.EXPECT().GetHandlers().Return(map[string]http.Handler{
		validRoutePath: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
	}).AnyTimes()
	return mockDebugConfig
}
func TestConfigureDebugServerStart(t *testing.T) {
	// set up a controller
	mockCtrl := gomock.NewController(t)
	stop := make(chan struct{})

	kubeClient, _, cfg, err := setupComponents(testOSMNamespace, testOSMConfigMapName, false, stop)
	if err != nil {
		t.Fatal(err)
	}

	con := &controller{
		debugServerRunning: false,
		debugComponents:    mockDebugConfig(mockCtrl),
		debugServer:        nil,
	}
	go con.configureDebugServer(cfg)

	updatedConfigMap := v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testOSMNamespace,
			Name:      testOSMConfigMapName,
		},
		Data: map[string]string{
			"enable_debug_server": "true",
		},
	}
	_, err = kubeClient.CoreV1().ConfigMaps(testOSMNamespace).Update(context.TODO(), &updatedConfigMap, metav1.UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	close(stop)
	if con.debugServerRunning == true {
		t.Error("Expected debugServerRunning to be true but was false")
	}

	if con.debugServer != nil {
		t.Error("Expected debugServer to not be nil but was nil")
	}

}

/*
func TestConfigureDebugServer(t *testing.T) {
	t.Skip()
	testCases := []struct {
		name                       string
		initialDebugServerEnabled  bool
		changeDebugServerEnabledTo bool
		controllerDebugServer      httpserver.DebugServerInterface
		c                          controller
		expectedStopCount          int
		expectedStopErr            bool
		expectedDebugServerRunning bool
		expectedDebugServerNil     bool
	}{
		{
			name:                       "turn on debug server",
			initialDebugServerEnabled:  false,
			changeDebugServerEnabledTo: true,
			controllerDebugServer:      nil,
			expectedStopCount:          0,
			expectedStopErr:            false,
			expectedDebugServerRunning: true,
			expectedDebugServerNil:     false,
		},
		{
			name:                       "turn off debug server",
			initialDebugServerEnabled:  true,
			changeDebugServerEnabledTo: false,
			expectedStopCount:          1,
			expectedStopErr:            false,
			expectedDebugServerRunning: false,
			expectedDebugServerNil:     false,
		},

		{
			name:                       "error when turning off debug server",
			initialDebugServerEnabled:  true,
			changeDebugServerEnabledTo: false,
			expectedStopCount:          1,
			expectedStopErr:            true,
			expectedDebugServerRunning: false,
			expectedDebugServerNil:     false,
		},
		{
			name:                       "debug server is already on, do nothing",
			initialDebugServerEnabled:  true,
			changeDebugServerEnabledTo: true,
			expectedStopCount:          0,
			expectedStopErr:            false,
			expectedDebugServerRunning: true,
			expectedDebugServerNil:     true,
		},
		{
			name:                       "debug server is already off, do nothing",
			initialDebugServerEnabled:  false,
			changeDebugServerEnabledTo: false,
			expectedStopCount:          0,
			expectedStopErr:            false,
			expectedDebugServerRunning: false,
			expectedDebugServerNil:     false,
		},
	}

	for _, tests := range testCases {
		assert := assert.New(t)

		t.Run(fmt.Sprintf("Test: %s", tests.name), func(t *testing.T) {

			fmt.Println(tests.name)

			stop := make(chan struct{})
			kubeClient, configMap, cfg, err := setupComponents(testOSMNamespace, testOSMConfigMapName, tests.initialDebugServerEnabled, stop)

			mockCtrl := gomock.NewController(t)
			mockDebugConfig := debugger.NewMockDebugServer(mockCtrl)
			mockDebugConfig.EXPECT().GetHandlers().Return(map[string]http.Handler{
				validRoutePath: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
			}).AnyTimes()

			testErr := errors.Errorf("Debug server error")
			contr := controller{
				debugServerRunning: tests.initialDebugServerEnabled,
				debugComponents:    mockDebugConfig,
			}
			if !tests.initialDebugServerEnabled {
				contr.debugServer = nil
			} else {
				fakeDebugServer := FakeDebugServer{
					stopCount: 0,
				}
				if tests.expectedStopErr {
					fakeDebugServer.stopErr = testErr
				}
				fakeDebugServer.wg.Add(1)
			}

			go tests.c.configureDebugServer(cfg)
// some loop that waits for wg.Done() ... nothing on f.wg and then check assertions
			//update configMap with change to enable_debug_server
			configMap.Data["enable_debug_server"] = strconv.FormatBool(tests.changeDebugServerEnabledTo)
			//defaultConfigMap["enable_debug_server"] = strconv.FormatBool(tests.changeDebugServerEnabledTo)
			//configMap.Data = defaultConfigMap
			_, err = kubeClient.CoreV1().ConfigMaps(testOSMNamespace).Update(context.TODO(), &configMap, metav1.UpdateOptions{})
			assert.Nil(err)
			//give time for goroutine to run
			// time.Sleep(time.Second)


				assert.Equal(tests.expectedDebugServerRunning, contr. debugServerRunning)

				if !tests.initialDebugServerEnabled && tests.changeDebugServerEnabledTo || tests.expectedStopErr {
					assert.NotNil(tests.c.debugServer)
				} else {
					assert.Nil(tests.c.debugServer)
				}

				if tests.expectedStopErr {
					assert.Equal(tests.expectedStopCount, fakeDebugServerGetErr.stopCount)
				} else {
					assert.Equal(tests.expectedStopCount, fakeDebugServer.stopCount)
					fakeDebugServer.stopCount = 0
				}

			close(stop)

				err = kubeClient.CoreV1().ConfigMaps(osmNamespace).Delete(context.TODO(), osmConfigMapName, metav1.DeleteOptions{})
				assert.Nil(err)

		})
	}
}
*/

/*
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
*/

func setupComponents(namespace, configMapName string, initialDebugServerEnabled bool, stop chan struct{}) (*testclient.Clientset, v1.ConfigMap, configurator.Configurator, error) {
	kubeClient := testclient.NewSimpleClientset()

	defaultConfigMap := map[string]string{
		"enable_debug_server": strconv.FormatBool(initialDebugServerEnabled),
	}
	configMap := v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      configMapName,
		},
		Data: defaultConfigMap,
	}
	_, err := kubeClient.CoreV1().ConfigMaps(namespace).Create(context.TODO(), &configMap, metav1.CreateOptions{})
	if err != nil {
		return kubeClient, configMap, nil, err
	}
	cfg := configurator.NewConfigurator(kubeClient, stop, namespace, configMapName)
	return kubeClient, configMap, cfg, nil
}
