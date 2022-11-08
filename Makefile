export KUBEBUILDER_ASSETS ?= ${HOME}/.kubebuilder/bin
export CLUSTER_NAME ?= $(shell kubectl config view --minify -o jsonpath='{.clusters[].name}' | rev | cut -d"/" -f1 | rev | cut -d"." -f1)
export CLUSTER_VPC_ID ?= $(shell aws eks describe-cluster --name $(CLUSTER_NAME) | jq -r ".cluster.resourcesVpcConfig.vpcId")
export AWS_ACCOUNT_ID ?= $(shell aws sts get-caller-identity --query Account --output text)

# Image URL to use all building/pushing image targets
IMG ?= controller:latest
ECRIMAGES ?=public.ecr.aws/m7r9p7b3/aws-gateway-controller:latest

# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.22

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Setting SHELL to bash allows bash commands to be executed by recipes.
# This is a requirement for 'setup-envtest.sh' in the test target.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk commands is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Local Development

.PHONY: run
run: ## Run in development mode
	go run main.go

.PHONY: presubmit
presubmit: vet test ## Run all commands before submitting code

.PHONY: vet
vet: ## Vet the code and dependencies
	go mod tidy
	go generate ./...
	go vet ./...
	go fmt ./...

.PHONY: test
test: ## Run tests.
	go test ./... -coverprofile coverage.out

.PHONY: toolchain
toolchain: ## Install developer toolchain
	./hack/toolchain.sh
	./setup.sh
	./scripts/gen_mocks.sh

##@ Deployment

.PHONY: docker-build
docker-build: test ## Build docker image with the manager.
	sudo docker build -t ${IMG} .

.PHONY: docker-push
docker-push: ## Push docker image with the manager.
	docker push ${IMG}

.PHONY: deploy
deployment: ## Create a deployment file that can be applied with `kubectl apply -f deploy.yaml`
	cd config/manager && kustomize edit set image controller=${ECRIMAGES}
	kustomize build config/default > deploy.yaml