default: build

registry := "ghcr.io/lukasngl/secret-manager"

# Show available recipes
help:
    @just --list

# Run all code generation
gen: generate-manifests generate-helm-chart

# Generate Go code and base manifests
generate-manifests:
    controller-gen object:headerFile="hack/boilerplate.go.txt" paths="./..."
    controller-gen crd paths="./..." output:crd:artifacts:config=config/crd
    controller-gen rbac:roleName=secret-manager paths="./..." output:rbac:artifacts:config=config/rbac

# Generate helm chart from manifests
generate-helm-chart:
    go run ./cmd/gen-crd > charts/secret-manager/crds/clientsecrets.yaml
    @printf '%s\n' 'apiVersion: rbac.authorization.k8s.io/v1' 'kind: ClusterRole' 'metadata:' '  name: {{{{ include "secret-manager.fullname" . }}' '  labels:' '    {{{{- include "secret-manager.labels" . | nindent 4 }}' > charts/secret-manager/templates/clusterrole.yaml
    @sed -n '/^rules:/,$p' config/rbac/role.yaml >> charts/secret-manager/templates/clusterrole.yaml

# Run go fmt
fmt:
    go fmt ./...

# Run go vet
vet:
    go vet ./...

# Run unit tests
test:
    go test ./... -coverprofile cover.out

# Run e2e tests
e2e:
    go test ./test/e2e/... -v -timeout 10m

# Run golangci-lint
lint *args:
    golangci-lint run {{ args }}

# Build manager binary
build: gen fmt vet
    go build -o bin/manager cmd/main.go

# Run controller locally
run: gen fmt vet
    go run ./cmd/main.go

# Build container image with nix
image-build:
    nix build .#image --print-out-paths --print-build-logs

# Push container image to registry
image-push *skopeo_args:
    #!/usr/bin/env bash
    set -euo pipefail
    image_script=$(nix build .#image --no-link --print-out-paths)
    tag=$(nix eval --raw .#image.imageTag)
    $image_script | skopeo copy {{ skopeo_args }} \
        docker-archive:/dev/stdin \
        docker://{{ registry }}:${tag}

# Install CRDs into cluster
install: gen
    kubectl apply -f charts/secret-manager/crds/

# Uninstall CRDs from cluster
uninstall:
    kubectl delete -f charts/secret-manager/crds/ --ignore-not-found
