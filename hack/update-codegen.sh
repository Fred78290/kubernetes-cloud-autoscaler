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
OUTPUT_BASE="${SCRIPT_ROOT}/../../../"

ls ${OUTPUT_BASE}

source "${CODEGEN_PKG}/kube_codegen.sh"

# generate the code with:
# --output-base    because this script should also be able to run inside the vendor dir of
#                  k8s.io/kubernetes. The output-base is needed for the generators to output into the vendor dir
#                  instead of the $GOPATH directly. For normal projects this can be dropped.
#bash "${CODEGEN_PKG}/generate-groups.sh" "deepcopy,client,informer,lister" \
#  github.com/Fred78290/kubernetes-cloud-autoscaler/pkg/generated \
#  github.com/Fred78290/kubernetes-cloud-autoscaler/pkg/apis \
#  nodemanager:v1alpha1 \
#  --output-base "${SCRIPT_ROOT}/../../../" \
#  --go-header-file "${SCRIPT_ROOT}"/hack/boilerplate.go.txt

kube::codegen::gen_helpers \
    --input-pkg-root github.com/Fred78290/kubernetes-cloud-autoscaler/pkg/apis \
    --output-base "${OUTPUT_BASE}" \
    --boilerplate "${SCRIPT_ROOT}/hack/boilerplate.go.txt"

kube::codegen::gen_client \
    --with-watch \
    --input-pkg-root github.com/Fred78290/kubernetes-cloud-autoscaler/pkg/apis \
    --output-pkg-root github.com/Fred78290/kubernetes-cloud-autoscaler/pkg/generated \
    --output-base "${OUTPUT_BASE}" \
    --boilerplate "${SCRIPT_ROOT}/hack/boilerplate.go.txt"

rm -rf vendor

go mod tidy -v
go mod vendor