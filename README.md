# Secret Manager Operator

[![CI](https://github.com/lukasngl/secret-manager/actions/workflows/ci.yaml/badge.svg)](https://github.com/lukasngl/secret-manager/actions/workflows/ci.yaml)
[![codecov](https://codecov.io/github/lukasngl/secret-manager/graph/badge.svg?token=KFUG301E2O)](https://codecov.io/github/lukasngl/secret-manager)
[![built with nix](https://img.shields.io/static/v1?logo=nixos&logoColor=white&label=&message=Built%20with%20Nix&color=41439a)](https://builtwithnix.org)

> **Work in Progress** - This operator is under active development and not yet production-ready.

A Kubernetes operator that automatically provisions and rotates secrets from external providers. Currently supports Azure AD client secrets with a plugin architecture for additional providers.

## How it Works

1. Create a `ClientSecret` custom resource specifying the provider and configuration
2. The operator provisions credentials from the external provider
3. Credentials are written to a Kubernetes Secret (specified by `secretRef`)
4. Credentials are automatically rotated before expiry
5. On `ClientSecret` deletion, the operator cleans up external credentials

```yaml
apiVersion: secret-manager.ngl.cx/v1alpha1
kind: ClientSecret
metadata:
  name: my-app
spec:
  provider: azure
  config:
    objectId: "00000000-0000-0000-0000-000000000000"
    validity: "2160h"  # 90 days
    template:
      AZURE_CLIENT_ID: "{{ .ClientID }}"
      AZURE_CLIENT_SECRET: "{{ .ClientSecret }}"
      AZURE_TENANT_ID: "your-tenant-id"  # hardcode static values
  secretRef:
    name: my-app-credentials
```

## Security Model

Access control is managed via Kubernetes RBAC with three permission levels:

| Permission | What it allows | Security implication |
|------------|----------------|----------------------|
| `clientsecret-editor-role` | Create/edit/delete `ClientSecret` resources | Can request credentials for any Azure AD app the operator has access to |
| `clientsecret-viewer-role` | Read `ClientSecret` resources | Can see which apps are configured, but not the actual secrets |
| Read `Secret` | Read the output Secret | Can access the provisioned credentials |

### Privilege Escalation Risk

Anyone with `clientsecret-editor-role` can request credentials for **any** Azure AD application that the operator's service principal has access to.

**Mitigations:**

1. **Use `Application.ReadWrite.OwnedBy` permission** (recommended)
   - The operator can only manage applications it owns
   - Explicitly transfer app ownership to the operator's service principal

2. **Limit operator's Azure permissions**
   - Only grant the operator access to specific applications
   - Use separate operators per trust boundary if needed

## Supported Adapters

| Provider | Status | Authentication |
|----------|--------|----------------|
| Azure AD | Working | DefaultAzureCredential (CLI, Env, Managed Identity, Workload Identity) |

## Adding Providers

Custom providers can be added by implementing the `Provider` interface in `internal/adapter/` and registering via `init()`. Each provider defines its own config schema using struct tags:

```go
// internal/adapter/myprovider/myprovider.go
package myprovider

import "github.com/lukasngl/secret-manager/internal/adapter"

type Config struct {
    ProjectID string `json:"projectId" jsonschema:"required"`
    // ...
}

var configSchema = adapter.MustSchema(Config{})

type Provider struct{}

func init() {
    adapter.DefaultRegistry().Register(&Provider{})
}

func (p *Provider) Type() string                  { return "myprovider" }
func (p *Provider) ConfigSchema() *adapter.Schema { return configSchema }
// ... implement Validate, Provision, DeleteKey
```

Run `just gen` to regenerate the CRD with the new provider's schema.

## Running Locally

```bash
# Using current kubeconfig context
go run ./cmd/main.go

# With specific context
go run ./cmd/main.go --context my-cluster

# With specific kubeconfig
go run ./cmd/main.go --kubeconfig ~/.kube/other-config
```

## Status

The operator tracks status in the `ClientSecret` resource:

```yaml
status:
  phase: Ready              # Ready, Pending, or Failed
  currentKeyId: "abc-123"
  activeKeys:
    - keyId: "abc-123"
      createdAt: "2025-01-06T12:00:00Z"
      expiresAt: "2025-04-06T12:00:00Z"
  failureCount: 0
  conditions:
    - type: Ready
      status: "True"
      reason: Provisioned
      message: "Credentials provisioned successfully"
```

View status with:
```bash
kubectl get clientsecrets   # or: kubectl get cs
kubectl describe cs my-app
```

## Installation

```bash
# Install CRD and operator via Helm
helm install secret-manager ./charts/secret-manager \
  --namespace secret-manager-system \
  --create-namespace \
  --set azure.workloadIdentity.enabled=true \
  --set azure.workloadIdentity.clientId="<your-client-id>"
```

## Roadmap

- [x] Unit and e2e tests
- [x] CI/CD pipeline with automated releases
- [ ] Prometheus metrics (provisioning latency, failure counts, key age)
- [ ] Kubernetes Events for provisioning/rotation/errors
- [ ] Additional providers (AWS Secrets Manager, GCP Secret Manager, HashiCorp Vault)
- [ ] Namespace-scoped ObjectID allowlists for fine-grained access control

## License

Copyright 2025. Licensed under the Apache License, Version 2.0.
