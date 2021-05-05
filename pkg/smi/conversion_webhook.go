package smi

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/munnerz/goautoneg"
	v1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
)

type conversionWebhook struct {
	kubeClient kubernetes.Interface
}

var scheme = runtime.NewScheme()

type mediaType struct {
	Type, SubType string
}

var serializers = map[mediaType]runtime.Serializer{
	{"application", "json"}: json.NewSerializer(json.DefaultMetaFactory, scheme, scheme, false),
	{"application", "yaml"}: json.NewYAMLSerializer(json.DefaultMetaFactory, scheme, scheme),
}

// convertFunc is the user defined function for any conversion. The code in this file is a
// template that can be use for any CR conversion given this function.
type convertFunc func(Object *unstructured.Unstructured, version string) (*unstructured.Unstructured, metav1.Status)

// NewConversionWebhook  starts a new web server handling SMI CRD conversion
func NewConversionWebhook(kubeClient kubernetes.Interface, stop <-chan struct{}) error {
	conv := &conversionWebhook{
		kubeClient: kubeClient,
	}
	go conv.Run(stop)

	return nil
}

func (c *conversionWebhook) Run(stop <-chan struct{}) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mux := http.NewServeMux()

	mux.HandleFunc("/smiconvert", c.ServeConvert)

	listenPort := 443
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", listenPort),
		Handler: mux,
	}

	log.Info().Msgf("Starting smi conversion webhook server on port: %v", listenPort)

	// wait for exit signals
	<-stop
	// Stop the server
	if err := server.Shutdown(ctx); err != nil {
		log.Error().Err(err).Msg("Error shutting down smi conversion webhook server")
	} else {
		log.Info().Msg("Done shutting down smi conversion webhook server")
	}
}

// ServeConvert servers endpoint for the smi converter defined as convertTrafficSplit function.
func (c *conversionWebhook) ServeConvert(w http.ResponseWriter, r *http.Request) {
	var body []byte
	if r.Body != nil {
		if data, err := ioutil.ReadAll(r.Body); err == nil {
			body = data
		}
	}
	contentType := r.Header.Get("Content-Type")
	serializer := getInputSerializer(contentType)
	if serializer == nil {
		msg := fmt.Sprintf("invalid Content-Type header `%s`", contentType)
		klog.Errorf(msg)
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	klog.V(2).Infof("handling request: %v", body)
	obj, gvk, err := serializer.Decode(body, nil, nil)
	if err != nil {
		msg := fmt.Sprintf("failed to deserialize body (%v) with error %v", string(body), err)
		klog.Error(err)
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	var responseObj runtime.Object
	switch *gvk {
	case v1.SchemeGroupVersion.WithKind("ConversionReview"):
		convertReview, ok := obj.(*v1.ConversionReview)
		if !ok {
			msg := fmt.Sprintf("Expected v1.ConversionReview but got: %T", obj)
			klog.Errorf(msg)
			http.Error(w, msg, http.StatusBadRequest)
			return
		}
		convertReview.Response = doConversionV1(convertReview.Request, convertExampleCRD)
		convertReview.Response.UID = convertReview.Request.UID
		klog.V(2).Info(fmt.Sprintf("sending response: %v", convertReview.Response))

		// reset the request, it is not needed in a response.
		convertReview.Request = &v1.ConversionRequest{}
		responseObj = convertReview
	default:
		msg := fmt.Sprintf("Unsupported group version kind: %v", gvk)
		klog.Error(err)
		http.Error(w, msg, http.StatusBadRequest)
		return
	}

	accept := r.Header.Get("Accept")
	outSerializer := getOutputSerializer(accept)
	if outSerializer == nil {
		msg := fmt.Sprintf("invalid accept header `%s`", accept)
		klog.Errorf(msg)
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	err = outSerializer.Encode(responseObj, w)
	if err != nil {
		klog.Error(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func getOutputSerializer(accept string) runtime.Serializer {
	if len(accept) == 0 {
		return serializers[mediaType{"application", "json"}]
	}

	clauses := goautoneg.ParseAccept(accept)
	for _, clause := range clauses {
		for k, v := range serializers {
			switch {
			case clause.Type == k.Type && clause.SubType == k.SubType,
				clause.Type == k.Type && clause.SubType == "*",
				clause.Type == "*" && clause.SubType == "*":
				return v
			}
		}
	}

	return nil
}

// doConversionV1 converts the requested objects in the v1 ConversionRequest using the given conversion function and
// returns a conversion response. Failures are reported with the Reason in the conversion response.
func doConversionV1(convertRequest *v1.ConversionRequest, convert convertFunc) *v1.ConversionResponse {
	var convertedObjects []runtime.RawExtension
	for _, obj := range convertRequest.Objects {
		cr := unstructured.Unstructured{}
		if err := cr.UnmarshalJSON(obj.Raw); err != nil {
			klog.Error(err)
			return &v1.ConversionResponse{
				Result: metav1.Status{
					Message: fmt.Sprintf("failed to unmarshall object (%v) with error: %v", string(obj.Raw), err),
					Status:  metav1.StatusFailure,
				},
			}
		}
		convertedCR, status := convert(&cr, convertRequest.DesiredAPIVersion)
		if status.Status != metav1.StatusSuccess {
			klog.Error(status.String())
			return &v1.ConversionResponse{
				Result: status,
			}
		}
		convertedCR.SetAPIVersion(convertRequest.DesiredAPIVersion)
		convertedObjects = append(convertedObjects, runtime.RawExtension{Object: convertedCR})
	}
	return &v1.ConversionResponse{
		ConvertedObjects: convertedObjects,
		Result:           statusSucceed(),
	}
}

func getInputSerializer(contentType string) runtime.Serializer {
	parts := strings.SplitN(contentType, "/", 2)
	if len(parts) != 2 {
		return nil
	}
	return serializers[mediaType{parts[0], parts[1]}]
}

func convertExampleCRD(Object *unstructured.Unstructured, toVersion string) (*unstructured.Unstructured, metav1.Status) {
	klog.V(2).Info("converting crd")

	//convertedObject := Object.DeepCopy()
	fromVersion := Object.GetAPIVersion()

	if toVersion == fromVersion {
		return nil, statusErrorWithMessage("conversion from a version to itself should not call the webhook: %s", toVersion)
	}

	switch Object.GetAPIVersion() {
	case "v1alpha2":
		switch toVersion {
		case "v1alpha4":
			return nil, statusErrorWithMessage("v1alpha2 -> %q not implemented", toVersion)
		default:
			return nil, statusErrorWithMessage("unexpected conversion version %q", toVersion)
		}
	case "v1alpha3":
		switch toVersion {
		case "v1alpha4":
			return nil, statusErrorWithMessage("v1alpha3 -> %q not implemented", toVersion)
		default:
			return nil, statusErrorWithMessage("unexpected conversion version %q", toVersion)
		}
	default:
		return nil, statusErrorWithMessage("unexpected conversion version %q", fromVersion)
	}
	//return convertedObject, statusSucceed()
}

func statusErrorWithMessage(msg string, params ...interface{}) metav1.Status {
	return metav1.Status{
		Message: fmt.Sprintf(msg, params...),
		Status:  metav1.StatusFailure,
	}
}

func statusSucceed() metav1.Status {
	return metav1.Status{
		Status: metav1.StatusSuccess,
	}
}
