# Show available recipes
default:
    @just --list

fix: tidy gen fmt (lint "--fix")

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

# Run golangci-lint
lint *args:
    find . -name go.mod -exec sh -c 'cd $(dirname "$1") && golangci-lint run {{ args }}' _ {} \;

# Install CRDs into cluster for a provider
install name: (_gen-chart name)
    kubectl apply -f provider-{{ name }}/charts/provider-{{ name }}/crds/

# Uninstall CRDs from cluster for a provider
uninstall name:
    kubectl delete -f provider-{{ name }}/charts/provider-{{ name }}/crds/ --ignore-not-found

# Print nix check matrix as JSON (used by CI)
_print-checks:
    @nix flake show --json 2>/dev/null | jq -c '[.checks."x86_64-linux" | to_entries[] | {check: .key}]'

# Print e2e test app matrix as JSON (used by CI)
_print-e2e-tests:
    @nix flake show --json 2>/dev/null | jq -c '[.apps."x86_64-linux" | to_entries[] | select(.key | startswith("e2e-test-")) | {app: .key}]'
