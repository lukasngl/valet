//go:generate godogen

package e2e

import (
	"context"
	"fmt"
	"time"

	"github.com/lukasngl/valet/framework/bddtest"
	"github.com/lukasngl/valet/provider-mock/api/v1alpha1"
	"github.com/lukasngl/valet/provider-mock/mock"
)

// Suite holds per-scenario state for mock-provider-specific steps.
// Common steps are handled by the embedded [bddtest.Suite].
type Suite struct {
	*bddtest.Suite[*v1alpha1.ClientSecret]
	provider *mock.Provider
}

//godogen:then ^the mock provider should have received at least (\d+) provision calls$
func (s *Suite) theMockProviderShouldHaveReceivedAtLeastProvisionCalls(
	_ context.Context,
	count int,
) error {
	actual := s.provider.ProvisionCount
	if actual < count {
		return fmt.Errorf("expected at least %d provision calls, got %d", count, actual)
	}
	return nil
}

//godogen:then ^the mock provider should have received at least (\d+) provision calls within (\d+) seconds$
func (s *Suite) theMockProviderShouldHaveReceivedAtLeastProvisionCallsWithin(
	_ context.Context,
	count, seconds int,
) error {
	deadline := time.Now().Add(time.Duration(seconds) * time.Second)

	var actual int
	for time.Now().Before(deadline) {
		actual = s.provider.ProvisionCount
		if actual >= count {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}

	return fmt.Errorf(
		"expected at least %d provision calls, got %d after %d seconds",
		count, actual, seconds,
	)
}

//godogen:then ^the mock provider should have received at least (\d+) delete key calls within (\d+) seconds$
func (s *Suite) theMockProviderShouldHaveReceivedAtLeastDeleteKeyCallsWithin(
	_ context.Context,
	count, seconds int,
) error {
	deadline := time.Now().Add(time.Duration(seconds) * time.Second)

	var actual int
	for time.Now().Before(deadline) {
		actual = len(s.provider.DeleteKeyCalls)
		if actual >= count {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}

	return fmt.Errorf(
		"expected at least %d delete key calls, got %d after %d seconds",
		count, actual, seconds,
	)
}
