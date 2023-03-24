package webhook

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"strconv"
	"sync"

	admissionv1 "k8s.io/api/admission/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apiserver/pkg/admission"
	"k8s.io/apiserver/pkg/authentication/user"
	admissionregistrationv1apply "k8s.io/client-go/applyconfigurations/admissionregistration/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

var logger klog.Logger = klog.LoggerWithName(klog.Background(), "webhook")

// PEM encoded certs and keys for webhook
type CertInfo struct {
	Cert []byte
	Key  []byte
	Root []byte
}

func NewCertInfoFromFiles(certFile, keyFile, rootFile string) (CertInfo, error) {
	rootCert, err := ioutil.ReadFile(rootFile)

	if err != nil {
		return CertInfo{}, fmt.Errorf("loading CA bundle %s: %w", rootFile, err)
	}

	keyData, err := ioutil.ReadFile(keyFile)
	if err != nil {
		return CertInfo{}, fmt.Errorf("loading key %s: %w", keyFile, err)
	}

	certData, err := ioutil.ReadFile(certFile)
	if err != nil {
		return CertInfo{}, fmt.Errorf("loading cert %s: %w", certFile, err)
	}

	return CertInfo{
		Cert: certData,
		Key:  keyData,
		Root: rootCert,
	}, nil
}

type Interface interface {
	Install(client kubernetes.Interface) error

	// Runs the webhook server until the passed context is cancelled, or it
	// experiences an internal error.
	//
	// Error is always non-nil and will always be one of:
	//		deadline exceeded
	//		context cancelled
	//		or http listen error
	Run(ctx context.Context) error
}

func New(port int, certs CertInfo, scheme *runtime.Scheme, validator admission.ValidationInterface) Interface {
	codecs := serializer.NewCodecFactory(scheme)

	return &webhook{
		CertInfo:         certs,
		objectInferfaces: admission.NewObjectInterfacesFromScheme(scheme),
		decoder:          codecs.UniversalDeserializer(),
		port:             port,
		validator:        validator,
	}
}

type webhook struct {
	lock             sync.Mutex
	port             int
	serverPort       int
	validator        admission.ValidationInterface
	objectInferfaces admission.ObjectInterfaces
	decoder          runtime.Decoder
	CertInfo
}

func (wh *webhook) Install(client kubernetes.Interface) error {
	wh.lock.Lock()
	defer wh.lock.Unlock()

	port := wh.serverPort
	if port == 0 {
		return errors.New("server is not running")
	}

	_, err := client.
		AdmissionregistrationV1().
		ValidatingWebhookConfigurations().
		Apply(
			context.TODO(),
			admissionregistrationv1apply.ValidatingWebhookConfiguration("cel-admission-polyfill.k8s.io").
				WithWebhooks(
					admissionregistrationv1apply.ValidatingWebhook().
						WithName("cel-admission-polyfill.k8s.io").
						WithRules(
							admissionregistrationv1apply.RuleWithOperations().
								WithScope("*").
								WithAPIGroups("*").
								WithAPIVersions("*").
								WithOperations("*").
								WithResources("*"),
						).
						WithAdmissionReviewVersions("v1").
						WithClientConfig(
							//TODO: When in cluster install a service too
							// and use a service for this
							admissionregistrationv1apply.WebhookClientConfig().
								WithURL("https://127.0.0.1:"+strconv.Itoa(int(port))+"/validate").
								WithCABundle(wh.Root...)).
						WithSideEffects(
							admissionregistrationv1.SideEffectClassNone).
						//!TODO: gate for debugging
						WithFailurePolicy(admissionregistrationv1.Ignore),
				),
			metav1.ApplyOptions{
				FieldManager: "cel_polyfill_debug",
			},
		)

	if err != nil {
		return fmt.Errorf("updating webhook configuration: %w", err)
	}
	return nil
}

func (wh *webhook) createListener() (net.Listener, int, error) {
	wh.lock.Lock()
	defer wh.lock.Unlock()
	if wh.serverPort != 0 {
		return nil, 0, errors.New("server is already running")
	}

	listener, err := net.Listen("tcp", ":"+strconv.Itoa(wh.port))
	if err != nil {
		return nil, 0, err
	}

	port := listener.Addr().(*net.TCPAddr).Port
	wh.serverPort = port
	return listener, wh.serverPort, nil
}

func (wh *webhook) Run(ctx context.Context) error {
	// Create a new certificate from the loaded certificate and key.
	serverCert, err := tls.X509KeyPair(wh.Cert, wh.Key)
	if err != nil {
		return err
	}

	// Create a new CertPool and add the CA certificate to it.
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(wh.Root)

	listener, port, err := wh.createListener()
	if err != nil {
		return err
	}

	defer func() {
		wh.lock.Lock()
		defer wh.lock.Unlock()
		wh.serverPort = 0
	}()

	fork, cancel := context.WithCancel(ctx)

	// Start server
	mux := http.NewServeMux()
	mux.HandleFunc("/health", wh.handleHealth)
	mux.HandleFunc("/validate", wh.handleWebhookValidate)
	mux.HandleFunc("/mutate", wh.handleWebhookMutate)

	srv := http.Server{}
	srv.Handler = mux
	srv.Addr = ":" + strconv.Itoa(port)
	srv.TLSConfig = &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.NoClientCert,
	}
	var serverError error

	go func() {
		serverError = srv.ServeTLS(listener, "", "")
		// ListenAndServeTLS always returns non-nil error
		cancel()
	}()

	logger.Info("started webhook HTTP server", "port", port)
	defer logger.Info("webhook server has stopped")
	<-fork.Done()

	// If the caller closed their context, rather than the server having errored,
	// close the server. srv.Close() is safe to call on an already-closed server
	//
	// note: should we prefer to use Shutdown with a deadline for graceful close
	// rather than Close?
	if err := srv.Close(); err != nil {
		// Errors with gracefully shutting down connections. Not fatal. Server
		// is still closed.
		logger.Error(err, "shutting down webhook")
	}

	// Prefer the passed context's error to pick up deadline/cancelled errors
	err = fork.Err()
	if err == nil {
		// If the fork was closed, but the passed in context was not
		// expired/cancelled, then the server experienced an error independently
		err = serverError
	}
	return err
}

func (wh *webhook) handleHealth(w http.ResponseWriter, req *http.Request) {
	fmt.Fprint(w, "OK")
}

func (wh *webhook) handleWebhookValidate(w http.ResponseWriter, req *http.Request) {
	parsed, err := parseRequest(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		logger.Error(err, "parsing admission review request")
		return
	}

	logger.Info(
		"review request",
		"resource",
		parsed.Request.Resource.String(),
		"namespace",
		parsed.Request.Namespace,
		"name",
		parsed.Request.Name,
		"uid",
		parsed.Request.UID,
	)

	failure := func(err error, status int) {
		http.Error(w, err.Error(), status)
		logger.Error(err, "review response", "uid", parsed.Request.UID, "status", status)
	}

	allowed := true
	errString := "valid"

	if wh.validator.Handles(admission.Operation(parsed.Request.Operation)) {
		var object runtime.Object
		var oldObject runtime.Object

		if len(parsed.Request.OldObject.Raw) > 0 {
			obj, gvk, err := wh.decoder.Decode(parsed.Request.OldObject.Raw, nil, nil)
			switch {
			case gvk == nil || *gvk != schema.GroupVersionKind(parsed.Request.Kind):
				// GVK case first. If object type is unknown it is parsed to
				// unstructured, but
				failure(fmt.Errorf("unexpected GVK %v. Expected %v", gvk, parsed.Request.Kind), http.StatusBadRequest)
				return
			case err != nil && runtime.IsNotRegisteredError(err):
				var oldUnstructured unstructured.Unstructured
				err = json.Unmarshal(parsed.Request.OldObject.Raw, &oldUnstructured)
				if err != nil {
					failure(err, http.StatusInternalServerError)
					return
				}

				oldObject = &oldUnstructured
			case err != nil:
				failure(err, http.StatusBadRequest)
				return
			default:
				oldObject = obj
			}
		}

		if len(parsed.Request.Object.Raw) > 0 {
			obj, gvk, err := wh.decoder.Decode(parsed.Request.Object.Raw, nil, nil)
			switch {
			case gvk == nil || *gvk != schema.GroupVersionKind(parsed.Request.Kind):
				// GVK case first. If object type is unknown it is parsed to
				// unstructured, but
				failure(fmt.Errorf("unexpected GVK %v. Expected %v", gvk, parsed.Request.Kind), http.StatusBadRequest)
				return
			case err != nil && runtime.IsNotRegisteredError(err):
				var objUnstructured unstructured.Unstructured
				err = json.Unmarshal(parsed.Request.Object.Raw, &objUnstructured)
				if err != nil {
					failure(err, http.StatusInternalServerError)
					return
				}

				object = &objUnstructured
			case err != nil:
				failure(err, http.StatusBadRequest)
				return
			default:
				object = obj
			}
		}

		// Parse into native types if possible
		convertExtra := func(input map[string]authenticationv1.ExtraValue) map[string][]string {
			if input == nil {
				return nil
			}

			res := map[string][]string{}
			for k, v := range input {
				var converted []string
				for _, s := range v {
					converted = append(converted, string(s))
				}
				res[k] = converted
			}
			return res
		}

		//!TODO: Parse options as v1.CreateOptions, v1.DeleteOptions, or v1.PatchOptions

		attrs := admission.NewAttributesRecord(
			object,
			oldObject,
			schema.GroupVersionKind(parsed.Request.Kind),
			parsed.Request.Namespace,
			parsed.Request.Name,
			schema.GroupVersionResource{
				Group:    parsed.Request.Resource.Group,
				Version:  parsed.Request.Resource.Version,
				Resource: parsed.Request.Resource.Resource,
			},
			parsed.Request.SubResource,
			admission.Operation(parsed.Request.Operation),
			nil, // operation options?
			false,
			&user.DefaultInfo{
				Name:   parsed.Request.UserInfo.Username,
				UID:    parsed.Request.UserInfo.UID,
				Groups: parsed.Request.UserInfo.Groups,
				Extra:  convertExtra(parsed.Request.UserInfo.Extra),
			})

		verr := wh.validator.Validate(context.TODO(), attrs, wh.objectInferfaces)
		allowed = verr == nil
		if verr != nil {
			errString = verr.Error()
		}
	}

	var status int32 = http.StatusAccepted
	if !allowed {
		status = http.StatusForbidden
	}
	response := reviewResponse(
		parsed.Request.UID,
		allowed,
		status,
		errString,
	)

	out, err := json.Marshal(response)
	if err != nil {
		failure(err, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(out)
	logger.Info(
		"review response",
		"resource",
		parsed.Request.Resource.String(),
		"namespace",
		parsed.Request.Namespace,
		"name",
		parsed.Request.Name,
		"allowed",
		allowed,
		"msg",
		errString,
		"uid",
		parsed.Request.UID,
	)
}

func (wh *webhook) handleWebhookMutate(w http.ResponseWriter, req *http.Request) {
	fmt.Println("mutate hit")
	w.WriteHeader(500)
}

func reviewResponse(uid types.UID, allowed bool, httpCode int32,
	reason string) *admissionv1.AdmissionReview {
	return &admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			Kind:       "AdmissionReview",
			APIVersion: "admission.k8s.io/v1",
		},
		Response: &admissionv1.AdmissionResponse{
			UID:     uid,
			Allowed: allowed,
			Result: &metav1.Status{
				Code:    httpCode,
				Message: reason,
			},
		},
	}
}

// parseRequest extracts an AdmissionReview from an http.Request if possible
func parseRequest(r *http.Request) (*admissionv1.AdmissionReview, error) {
	if r.Header.Get("Content-Type") != "application/json" {
		return nil, fmt.Errorf("Content-Type: %q should be %q",
			r.Header.Get("Content-Type"), "application/json")
	}

	bodybuf := new(bytes.Buffer)
	bodybuf.ReadFrom(r.Body)
	body := bodybuf.Bytes()

	if len(body) == 0 {
		return nil, fmt.Errorf("admission request body is empty")
	}

	var a admissionv1.AdmissionReview

	if err := json.Unmarshal(body, &a); err != nil {
		return nil, fmt.Errorf("could not parse admission review request: %v", err)
	}

	if a.Request == nil {
		return nil, fmt.Errorf("admission review can't be used: Request field is nil")
	}

	return &a, nil
}
