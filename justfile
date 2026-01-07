default: build

img := "ghcr.io/lukasngl/secret-manager:latest"

# Show available recipes
help:
    @just --list

# Run all code generation
gen: generate manifests

# Generate DeepCopy implementations
generate:
    controller-gen object:headerFile="hack/boilerplate.go.txt" paths="./..."

# Generate CRD manifests and Helm chart
manifests:
    go run ./cmd/gen-kustomize -out config/crd/patches/config-schema.yaml
    controller-gen rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases
    kustomize build config/crd > charts/secret-manager/crds/clientsecrets.yaml

# Run go fmt
fmt:
    go fmt ./...

# Run go vet
vet:
    go vet ./...

# Run tests
test:
    go test ./... -coverprofile cover.out

# Run golangci-lint
lint:
    golangci-lint run

# Run golangci-lint with fixes
lint-fix:
    golangci-lint run --fix

# Build manager binary
build: gen fmt vet
    go build -o bin/manager cmd/main.go

# Run controller locally
run: gen fmt vet
    go run ./cmd/main.go

# Build docker image
docker-build:
    docker build -t {{ img }} .

# Install CRDs into cluster
install: manifests
    kubectl apply -f charts/secret-manager/crds/

# Uninstall CRDs from cluster
uninstall:
    kubectl delete -f charts/secret-manager/crds/ --ignore-not-found
