#!/usr/bin/env bash

# Copyright 2017 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_ROOT=$(dirname "${BASH_SOURCE[0]}")/..
CODEGEN_PKG=${CODEGEN_PKG:-$(cd "${SCRIPT_ROOT}"; ls -d -1 ./vendor/k8s.io/code-generator 2>/dev/null || echo ../code-generator)}


function codegen::join() { local IFS="$1"; shift; echo "$*"; }

PKG_NAME="github.com/alexzielenski/cel_polyfill"

GROUPS_WITH_VERSIONS="celadmissionpolyfill.k8s.io:v0alpha1,v0alpha2"

APIS_PKG="${PKG_NAME}/pkg/apis"
OUTPUT_PKG="pkg/generated"
BOILERPLATE="${SCRIPT_ROOT}"/hack/boilerplate.go.txt

chmod +x "${CODEGEN_PKG}"/generate-groups.sh

# generate the code with:
# --output-base    because this script should also be able to run inside the vendor dir of
#                  k8s.io/kubernetes. The output-base is needed for the generators to output into the vendor dir
#                  instead of the $GOPATH directly. For normal projects this can be dropped.
"${CODEGEN_PKG}"/generate-groups.sh "informer,client,lister" \
  ${PKG_NAME}/${OUTPUT_PKG} $APIS_PKG \
  "$GROUPS_WITH_VERSIONS" \
  --go-header-file $BOILERPLATE \
  --output-base ${SCRIPT_ROOT}/../../../

# For some reason register-gen is not included in the above code generators?
pushd $SCRIPT_ROOT >/dev/null

# enumerate group versions
FQ_APIS=() # e.g. k8s.io/api/apps/v1
for GVs in ${GROUPS_WITH_VERSIONS}; do
  IFS=: read -r G Vs <<<"${GVs}"

  # enumerate versions
  for V in ${Vs//,/ }; do
    FQ_APIS+=("./pkg/apis/${G}/${V}")
  done
done


echo "Generating register files for ${GROUPS_WITH_VERSIONS}"
go run k8s.io/code-generator/cmd/register-gen \
  --input-dirs $(codegen::join , "${FQ_APIS[@]}") \
  --output-file-base zz_generated.register \
  --go-header-file $BOILERPLATE \
  --output-base .

echo "Generating deepcopy files for ${GROUPS_WITH_VERSIONS}"
go run k8s.io/code-generator/cmd/deepcopy-gen \
  --input-dirs $(codegen::join , "${FQ_APIS[@]}") \
  --output-file-base zz_generated.deepcopy \
  --go-header-file $BOILERPLATE \
  --output-base .

# Generate CRD manifests for all types using controller-gen
echo "Generating crd manifests for ${GROUPS_WITH_VERSIONS}"
go run sigs.k8s.io/controller-tools/cmd/controller-gen \
  crd \
  paths=./pkg/apis/... \
  output:dir=./crds

popd >/dev/null
