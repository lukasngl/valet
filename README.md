# Client Secret Operator (CSO)

[![CI](https://github.com/lukasngl/client-secret-operator/actions/workflows/ci.yaml/badge.svg)](https://github.com/lukasngl/client-secret-operator/actions/workflows/ci.yaml)
[![built with nix](https://img.shields.io/static/v1?logo=nixos&logoColor=white&label=&message=Built%20with%20Nix&color=41439a)](https://builtwithnix.org)

> **Work in Progress** - This operator is under active development and not yet production-ready.

A Kubernetes operator that automatically provisions and rotates client credentials from external identity providers. Built as a shared framework with per-provider modules.

## Architecture

```
framework/           Shared reconciler, types, and provider interface
provider-azure/      Azure Entra ID provider
provider-mock/       Mock provider for testing
```

Each provider ships as an independent binary with its own CRD, Helm chart, and RBAC. The framework handles the full credential lifecycle (provisioning, rotation, cleanup, finalizers) — providers only implement three methods.

## How it Works

1. Create a provider-specific custom resource (e.g. `AzureClientSecret`)
2. The operator provisions credentials from the external provider
3. Credentials are written to a Kubernetes Secret (specified by `secretRef`)
4. Credentials are automatically rotated before expiry
5. On deletion, the operator cleans up external credentials

```yaml
apiVersion: cso.ngl.cx/v1alpha1
kind: AzureClientSecret
metadata:
  name: my-app
spec:
  objectId: "00000000-0000-0000-0000-000000000000"
  validity: "2160h"  # 90 days
  template:
    AZURE_CLIENT_ID: "{{ .ClientID }}"
    AZURE_CLIENT_SECRET: "{{ .ClientSecret }}"
    AZURE_TENANT_ID: "your-tenant-id"
  secretRef:
    name: my-app-credentials
```

## Security Model

Access control is managed via Kubernetes RBAC:

| Permission | What it allows | Security implication |
|------------|----------------|----------------------|
| Create/edit CRD | Request credentials for configured apps | Can provision secrets for any app the operator has access to |
| Read CRD | See which apps are configured | No access to actual secrets |
| Read Secret | Read the output Secret | Access to provisioned credentials |

### Privilege Escalation Risk

Anyone who can create a provider CRD can request credentials for **any** application that the operator's service principal has access to.

**Mitigations:**
1. **Use `Application.ReadWrite.OwnedBy` permission** (recommended) — the operator can only manage applications it owns
2. **Limit operator permissions** — only grant access to specific applications
3. **Separate operators per trust boundary** if needed

## Providers

| Provider | Status | Authentication |
|----------|--------|----------------|
| Azure Entra ID | Working | DefaultAzureCredential (CLI, Env, Managed Identity, Workload Identity) |

## Adding Providers

Implement the `framework.Provider[O]` interface:

```go
type Provider[O Object] interface {
    NewObject() O
    Provision(ctx context.Context, obj O) (*Result, error)
    DeleteKey(ctx context.Context, obj O, keyID string) error
}
```

Each provider defines its own CRD type implementing `framework.Object`, with typed spec fields — no JSON marshaling in the hot path. See `provider-mock/` for a complete example.

## Installation

```bash
helm install cso-provider-azure oci://ghcr.io/lukasngl/client-secret-operator/charts/provider-azure \
  --namespace cso-system \
  --create-namespace
```

## Development

```bash
nix develop              # enter dev shell
just gen                 # regenerate CRDs, RBAC, Helm chart
just test                # run unit tests
just lint                # run golangci-lint
just e2e                 # run e2e tests
```

## Status

The operator tracks status in the provider CRD:

```yaml
status:
  phase: Ready
  currentKeyId: "abc-123"
  activeKeys:
    - keyId: "abc-123"
      createdAt: "2025-01-06T12:00:00Z"
      expiresAt: "2025-04-06T12:00:00Z"
  conditions:
    - type: Ready
      status: "True"
      reason: Provisioned
```

## Related Work

- [External Secrets Operator](https://external-secrets.io/) — syncs secrets from vaults (read-only); CSO provisions and rotates credentials (read-write)
- [Crossplane](https://www.crossplane.io/) — general-purpose infrastructure provisioning; CSO is focused on client credential lifecycle

## License

Copyright 2025. Licensed under the Apache License, Version 2.0.
