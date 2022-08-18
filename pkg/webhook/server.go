package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"

	"github.com/alexzielenski/cel_polyfill/pkg/validator"
	admissionv1 "k8s.io/api/admission/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	admissionregistrationv1apply "k8s.io/client-go/applyconfigurations/admissionregistration/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

type CertInfo struct {
	CertFile string
	KeyFile  string
	RootFile string
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

func New(certs CertInfo, validator validator.Interface) Interface {
	return &webhook{
		CertInfo:  certs,
		port:      9091,
		validator: validator,
	}
}

type webhook struct {
	port      uint16
	validator validator.Interface
	CertInfo
}

func (wh *webhook) Install(client kubernetes.Interface) error {
	rootCert, err := ioutil.ReadFile(wh.RootFile)

	if err != nil {
		return fmt.Errorf("loading CA bundle: %w", err)
	}

	_, err = client.
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
								WithURL("https://127.0.0.1:"+strconv.Itoa(int(wh.port))+"/validate").
								WithCABundle(rootCert...)).
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

func (wh *webhook) Run(ctx context.Context) error {
	fmt.Printf("starting webhook HTTP server on port %v\n", wh.port)
	defer fmt.Println("webhook server has stopped")

	fork, cancel := context.WithCancel(ctx)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", wh.handleHealth)
	mux.HandleFunc("/validate", wh.handleWebhookValidate)
	mux.HandleFunc("/mutate", wh.handleWebhookMutate)

	srv := http.Server{}
	srv.Handler = mux
	srv.Addr = ":" + strconv.Itoa(int(wh.port))

	var serverError error

	go func() {
		serverError = srv.ListenAndServeTLS(wh.CertFile, wh.KeyFile)
		// ListenAndServeTLS always returns non-nil error
		cancel()
	}()

	<-fork.Done()

	// If the caller closed their context, rather than the server having errored,
	// close the server. srv.Close() is safe to call on an already-closed server
	//
	// note: should we prefer to use Shutdown with a deadline for graceful close
	// rather than Close?
	if err := srv.Close(); err != nil {
		// Errors with gracefully shutting down connections. Not fatal. Server
		// is still closed.
		klog.Error(err)
	}

	// Prefer the passed context's error to pick up deadline/cancelled errors
	err := ctx.Err()
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
		return
	}

	klog.Info("admission review requested")

	var object unstructured.Unstructured
	var oldObject unstructured.Unstructured

	err = json.Unmarshal(parsed.Request.OldObject.Raw, &oldObject)
	// if err != nil {
	// 	// http.Error(w, err.Error(), http.StatusInternalServerError)
	// 	return
	// }
	err = json.Unmarshal(parsed.Request.Object.Raw, &object)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	err = wh.validator.Validate(parsed.Request.Resource, &oldObject, &object)
	allowed := err == nil || err.Error() != "validation failed"
	errString := "valid"
	if err != nil {
		errString = err.Error()
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(out)
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
