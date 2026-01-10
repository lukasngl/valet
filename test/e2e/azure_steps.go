package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/cucumber/godog"
	"github.com/lukasngl/secret-manager/internal/adapter"
	azure "github.com/lukasngl/secret-manager/internal/adapter/azure"
	"sigs.k8s.io/yaml"
)

// azureTestState holds state for Azure provider tests.
type azureTestState struct {
	provider     *azure.Provider
	result       *adapter.Result
	provisionErr error
	validateErr  error
	deleteErr    error
}

type azureStateKey struct{}

func getAzureState(ctx context.Context) *azureTestState {
	if state, ok := ctx.Value(azureStateKey{}).(*azureTestState); ok {
		return state
	}
	return nil
}

func setAzureState(ctx context.Context, state *azureTestState) context.Context {
	return context.WithValue(ctx, azureStateKey{}, state)
}

// azureEnvVars are the required environment variables for Azure tests.
var azureEnvVars = []string{
	"AZURE_CLIENT_ID",
	"AZURE_CLIENT_SECRET",
	"AZURE_TENANT_ID",
	"TEST_AZURE_OWNED_APP_OBJECT_ID",
	"TEST_AZURE_OTHER_APP_OBJECT_ID",
}

// azureConfigured returns true if all required Azure env vars are set.
func azureConfigured() bool {
	for _, v := range azureEnvVars {
		if os.Getenv(v) == "" {
			return false
		}
	}
	return true
}

//godogen:given ^Azure credentials are configured$
func azureCredentialsAreConfigured(ctx context.Context) (context.Context, error) {
	if !azureConfigured() {
		return ctx, godog.ErrSkip
	}
	state := &azureTestState{provider: &azure.Provider{}}
	return setAzureState(ctx, state), nil
}

// === Validation Steps ===

//godogen:when ^I validate config:$
func iValidateConfig(ctx context.Context, configYAML *godog.DocString) error {
	state := getAzureState(ctx)
	expanded := expandEnvVars(configYAML.Content)

	var configMap map[string]any
	if err := yaml.Unmarshal([]byte(expanded), &configMap); err != nil {
		return fmt.Errorf("parsing config YAML: %w", err)
	}

	configJSON, err := json.Marshal(configMap)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	state.validateErr = state.provider.Validate(configJSON)
	return nil
}

//godogen:then ^validation should fail with "([^"]*)"$
func validationShouldFailWith(ctx context.Context, expected string) error {
	state := getAzureState(ctx)
	if state.validateErr == nil {
		return fmt.Errorf("expected validation to fail with %q, but it succeeded", expected)
	}
	if !strings.Contains(strings.ToLower(state.validateErr.Error()), strings.ToLower(expected)) {
		return fmt.Errorf("expected error containing %q, got: %v", expected, state.validateErr)
	}
	return nil
}

//godogen:then ^validation should succeed$
func validationShouldSucceed(ctx context.Context) error {
	state := getAzureState(ctx)
	if state.validateErr != nil {
		return fmt.Errorf("expected validation to succeed, got: %v", state.validateErr)
	}
	return nil
}

// === Provisioning Steps ===

//godogen:when ^I provision credentials for app "([^"]*)" with:$
func iProvisionCredentialsForAppWith(ctx context.Context, objectID string, configYAML *godog.DocString) error {
	state := getAzureState(ctx)
	objectID = expandEnvVars(objectID)

	var partial struct {
		Validity string            `json:"validity"`
		Template map[string]string `json:"template"`
	}
	if err := yaml.Unmarshal([]byte(configYAML.Content), &partial); err != nil {
		return fmt.Errorf("parsing config YAML: %w", err)
	}

	config := azure.Config{
		ObjectID: objectID,
		Validity: partial.Validity,
		Template: partial.Template,
	}

	configJSON, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	state.result, state.provisionErr = state.provider.Provision(ctx, configJSON)
	return nil
}

//godogen:given ^I have provisioned credentials for app "([^"]*)"$
func iHaveProvisionedCredentialsForApp(ctx context.Context, objectID string) error {
	state := getAzureState(ctx)
	objectID = expandEnvVars(objectID)

	config := azure.Config{
		ObjectID: objectID,
		Validity: "1h",
		Template: map[string]string{"SECRET": "{{ .ClientSecret }}"},
	}

	configJSON, _ := json.Marshal(config)
	state.result, state.provisionErr = state.provider.Provision(ctx, configJSON)
	if state.provisionErr != nil {
		return fmt.Errorf("setup failed: %w", state.provisionErr)
	}
	return nil
}

//godogen:then ^the provisioning should succeed$
func theProvisioningShouldSucceed(ctx context.Context) error {
	state := getAzureState(ctx)
	if state.provisionErr != nil {
		return fmt.Errorf("expected success, got: %v", state.provisionErr)
	}
	if state.result == nil {
		return fmt.Errorf("expected result, got nil")
	}
	return nil
}

//godogen:then ^the provisioning should fail with "([^"]*)"$
func theProvisioningShouldFailWith(ctx context.Context, expected string) error {
	state := getAzureState(ctx)
	if state.provisionErr == nil {
		return fmt.Errorf("expected error containing %q, but succeeded", expected)
	}
	if !strings.Contains(strings.ToLower(state.provisionErr.Error()), strings.ToLower(expected)) {
		return fmt.Errorf("expected error containing %q, got: %v", expected, state.provisionErr)
	}
	return nil
}

//godogen:then ^the result should contain key "([^"]*)"$
func theResultShouldContainKey(ctx context.Context, key string) error {
	state := getAzureState(ctx)
	if state.result == nil {
		return fmt.Errorf("no result available")
	}
	if _, ok := state.result.StringData[key]; !ok {
		return fmt.Errorf("result missing key %q", key)
	}
	return nil
}

//godogen:then ^the result should have a valid key ID$
func theResultShouldHaveAValidKeyID(ctx context.Context) error {
	state := getAzureState(ctx)
	if state.result == nil {
		return fmt.Errorf("no result available")
	}
	if state.result.KeyID == "" {
		return fmt.Errorf("expected non-empty KeyID")
	}
	return nil
}

//godogen:then ^the result key "([^"]*)" should match pattern "([^"]*)"$
func theResultKeyShouldMatchPattern(ctx context.Context, key, pattern string) error {
	state := getAzureState(ctx)
	if state.result == nil {
		return fmt.Errorf("no result available")
	}
	value, ok := state.result.StringData[key]
	if !ok {
		return fmt.Errorf("result missing key %q", key)
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Errorf("invalid pattern %q: %w", pattern, err)
	}
	if !re.MatchString(value) {
		return fmt.Errorf("key %q value %q does not match pattern %q", key, value, pattern)
	}
	return nil
}

//godogen:then ^the result key "([^"]*)" should equal "([^"]*)"$
func theResultKeyShouldEqual(ctx context.Context, key, expected string) error {
	state := getAzureState(ctx)
	if state.result == nil {
		return fmt.Errorf("no result available")
	}
	value, ok := state.result.StringData[key]
	if !ok {
		return fmt.Errorf("result missing key %q", key)
	}
	if value != expected {
		return fmt.Errorf("key %q: expected %q, got %q", key, expected, value)
	}
	return nil
}

//godogen:then ^the result key "([^"]*)" should be a number greater than (\d+)$
func theResultKeyShouldBeNumberGreaterThan(ctx context.Context, key string, min int) error {
	state := getAzureState(ctx)
	if state.result == nil {
		return fmt.Errorf("no result available")
	}
	value, ok := state.result.StringData[key]
	if !ok {
		return fmt.Errorf("result missing key %q", key)
	}
	num, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("key %q value %q is not a number", key, value)
	}
	if num <= min {
		return fmt.Errorf("key %q value %d is not greater than %d", key, num, min)
	}
	return nil
}

// === Deletion Steps ===

//godogen:when ^I delete the provisioned key$
func iDeleteTheProvisionedKey(ctx context.Context) error {
	state := getAzureState(ctx)
	if state.result == nil {
		return fmt.Errorf("no credentials provisioned")
	}

	objectID := expandEnvVars("${TEST_AZURE_OWNED_APP_OBJECT_ID}")
	config := azure.Config{ObjectID: objectID}
	configJSON, _ := json.Marshal(config)

	state.deleteErr = state.provider.DeleteKey(ctx, configJSON, state.result.KeyID)
	return nil
}

//godogen:when ^I delete key "([^"]*)" for app "([^"]*)"$
func iDeleteKeyForApp(ctx context.Context, keyID, objectID string) error {
	state := getAzureState(ctx)
	objectID = expandEnvVars(objectID)

	config := azure.Config{ObjectID: objectID}
	configJSON, _ := json.Marshal(config)

	state.deleteErr = state.provider.DeleteKey(ctx, configJSON, keyID)
	return nil
}

//godogen:then ^the deletion should succeed$
func theDeletionShouldSucceed(ctx context.Context) error {
	state := getAzureState(ctx)
	if state.deleteErr != nil {
		return fmt.Errorf("expected success, got: %v", state.deleteErr)
	}
	return nil
}

//godogen:then ^the deletion should fail with "([^"]*)"$
func theDeletionShouldFailWith(ctx context.Context, expected string) error {
	state := getAzureState(ctx)
	if state.deleteErr == nil {
		return fmt.Errorf("expected error containing %q, but succeeded", expected)
	}
	if !strings.Contains(strings.ToLower(state.deleteErr.Error()), strings.ToLower(expected)) {
		return fmt.Errorf("expected error containing %q, got: %v", expected, state.deleteErr)
	}
	return nil
}
