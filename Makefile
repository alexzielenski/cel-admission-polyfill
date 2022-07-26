ifndef $(GOPATH)
    GOPATH=$(shell go env GOPATH)
endif

GENERATE_TLS_CERT = $(GOPATH)/bin/generate-tls-cert

$(GENERATE_TLS_CERT):
	go install github.com/Shyp/generate-tls-cert

certs/leaf.pem: | $(GENERATE_TLS_CERT)
	mkdir -p certs
	cd certs && $(GENERATE_TLS_CERT) --host=localhost,127.0.0.1

# Generate TLS certificates for local development.
generate_cert: certs/leaf.pem | $(GENERATE_TLS_CERT)

# codegen: 