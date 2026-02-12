registry := "ghcr.io/lukasngl/client-secret-operator"

# Show available recipes
default:
    @just --list

# Run all code generation
gen: (_gen-chart "azure") (_gen-chart "mock")

# Generate CRD, RBAC, and update Helm chart for a provider
_gen-chart name:
    controller-gen crd paths="./provider-{{ name }}/..." output:crd:artifacts:config=provider-{{ name }}/config/crd
    controller-gen rbac:roleName=provider-{{ name }} paths="./provider-{{ name }}/..." output:rbac:artifacts:config=provider-{{ name }}/config/rbac
    cp provider-{{ name }}/config/crd/*.yaml provider-{{ name }}/charts/provider-{{ name }}/crds/
    @printf '%s\n' \
      'apiVersion: rbac.authorization.k8s.io/v1' \
      'kind: ClusterRole' \
      'metadata:' \
      '  name: {{{{ include "provider-{{ name }}.fullname" . }}' \
      '  labels:' \
      '    {{{{- include "provider-{{ name }}.labels" . | nindent 4 }}' \
      > provider-{{ name }}/charts/provider-{{ name }}/templates/clusterrole.yaml
    @sed -n '/^rules:/,$p' provider-{{ name }}/config/rbac/role.yaml \
      >> provider-{{ name }}/charts/provider-{{ name }}/templates/clusterrole.yaml

# Run treefmt
fmt:
    nix fmt

# Run go mod tidy
tidy:
    find . -name go.mod -exec sh -c 'cd $(dirname {}); go mod tidy ' \;

# Run unit tests
test:
    go test ./... -coverprofile cover.out

# Run e2e tests
e2e:
    go test ./test/e2e/... -v -timeout 10m

# Run golangci-lint
lint *args:
    golangci-lint run {{ args }}

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

# Install CRDs into cluster for a provider
install name: (_gen-chart name)
    kubectl apply -f provider-{{ name }}/charts/provider-{{ name }}/crds/

# Uninstall CRDs from cluster for a provider
uninstall name:
    kubectl delete -f provider-{{ name }}/charts/provider-{{ name }}/crds/ --ignore-not-found
