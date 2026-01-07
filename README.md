# Secret Manager Operator

> **Work in Progress** - This operator is under active development and not yet production-ready.

A Kubernetes operator that automatically provisions and rotates secrets from external providers. Currently supports Azure AD client secrets with a plugin architecture for additional providers.

## How it Works

1. Create a Kubernetes Secret with management annotations
2. The operator detects it and provisions credentials from the external provider
3. Credentials are automatically rotated before expiry
4. On Secret deletion, the operator cleans up external credentials

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: my-app-credentials
  annotations:
    secret-manager.ngl.cx/managed: "true"
    secret-manager.ngl.cx/type: "azure"
    azure.secret-manager.ngl.cx/object-id: "00000000-0000-0000-0000-000000000000"
    azure.secret-manager.ngl.cx/validity: "2160h"  # 90 days
    azure.secret-manager.ngl.cx/client-id-target: "/data/AZURE_CLIENT_ID"
    azure.secret-manager.ngl.cx/client-secret-target: "/data/AZURE_CLIENT_SECRET"
type: Opaque
```

## Security Considerations

> **Warning**: Review these security implications before deploying.

### Privilege Escalation Risk

Any user who can create or edit Secrets with the managed annotation can potentially request credentials for **any** Azure AD application that the operator's service principal has access to.

**Attack scenario:**
1. Attacker creates a managed Secret with `object-id` of a high-privilege application
2. Operator provisions credentials for that application
3. Attacker reads the Secret and gains access to that application's permissions

### Mitigations

1. **Use `Application.ReadWrite.OwnedBy` permission** (recommended)
   - The operator can only manage applications it owns
   - Admin must explicitly transfer app ownership to the operator's service principal

2. **Namespace-to-ObjectID policy** (not yet implemented)
   - Restrict which namespaces can request which object IDs
   - Central policy controlled by cluster admins

3. **Workload Identity / Managed Identity**
   - Avoid storing Azure credentials in the cluster
   - Use Azure's native identity federation

## Supported Adapters

| Provider | Status | Authentication |
|----------|--------|----------------|
| Azure AD | Working | DefaultAzureCredential (CLI, Env, Managed Identity, Workload Identity) |

## Plugin Architecture

Custom adapters can be created by implementing the `Adapter` interface and registering via `init()`:

```go
package main

import (
    _ "github.com/lukasngl/client-secret-operator/pkg/adapter/azure"  // built-in
    _ "github.com/mycompany/vault-adapter"                            // custom

    "github.com/lukasngl/client-secret-operator/pkg/operator"
)

func main() {
    operator.Run()
}
```

## Running Locally

```bash
# Using current kubeconfig context
go run ./cmd/main.go

# With specific context
go run ./cmd/main.go --context my-cluster

# With specific kubeconfig
go run ./cmd/main.go --kubeconfig ~/.kube/other-config
```

## Status Annotations

The operator sets these annotations on managed Secrets:

| Annotation | Description |
|------------|-------------|
| `secret-manager.ngl.cx/status` | `ready`, `error`, or `pending` |
| `secret-manager.ngl.cx/provisioned-at` | Timestamp of last provisioning |
| `secret-manager.ngl.cx/valid-until` | When the current credentials expire |
| `secret-manager.ngl.cx/error` | Error message if status is `error` |
| `secret-manager.ngl.cx/managed-keys` | JSON array of provisioned key IDs (for cleanup) |

## TODO

- [ ] Namespace-to-ObjectID policy for access control
- [ ] Helm chart / Kustomize deployment manifests
- [ ] Metrics and observability
- [ ] Additional adapters (AWS, GCP, Vault)
- [ ] Admission webhook for validation

## License

Copyright 2025. Licensed under the Apache License, Version 2.0.
