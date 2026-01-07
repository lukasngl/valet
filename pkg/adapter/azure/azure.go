// Package azure provides the Azure AD adapter for the secret manager.
// Import this package to register the Azure adapter:
//
//	import _ "github.com/lukasngl/client-secret-operator/pkg/adapter/azure"
package azure

import (
	"fmt"

	"github.com/lukasngl/client-secret-operator/internal/adapter/azure"
	"github.com/lukasngl/client-secret-operator/pkg/adapter"
)

func init() {
	a, err := azure.New()
	if err != nil {
		fmt.Printf("azure adapter: failed to initialize: %v\n", err)
		return
	}
	adapter.Register(a)
}
