Feature: Secret Provisioning
  As a platform operator
  I want the mock provider to provision and manage secrets
  So that I can verify the framework's reconciliation logic

  Background:
    Given a Kubernetes cluster is running
    And the CRDs are installed
    And the operator is running

  Scenario: Provision a secret successfully
    When I create a ClientSecret:
      """yaml
      apiVersion: mock.valet.ngl.cx/v1alpha1
      kind: ClientSecret
      metadata:
        name: test-secret
      spec:
        secretRef:
          name: test-secret
        secretData:
          API_KEY: "test-api-key-123"
          API_SECRET: "test-api-secret-456"
      """
    Then the ClientSecret "test-secret" should have phase "Ready" within 30 seconds
    And a Secret "test-secret" should exist
    And the Secret "test-secret" should contain key "API_KEY" with value "test-api-key-123"
    And the Secret "test-secret" should contain key "API_SECRET" with value "test-api-secret-456"

  Scenario: Handle provider failure gracefully
    When I create a ClientSecret:
      """yaml
      apiVersion: mock.valet.ngl.cx/v1alpha1
      kind: ClientSecret
      metadata:
        name: failing-secret
      spec:
        secretRef:
          name: failing-secret
        shouldFailProvision: true
        secretData:
          KEY: "value"
      """
    Then the ClientSecret "failing-secret" should have phase "Failed" within 30 seconds
    And the ClientSecret "failing-secret" status should contain message "mock provider failure"
    And the Secret "failing-secret" should not exist

  Scenario: Delete ClientSecret cleans up resources
    When I create a ClientSecret:
      """yaml
      apiVersion: mock.valet.ngl.cx/v1alpha1
      kind: ClientSecret
      metadata:
        name: delete-test
      spec:
        secretRef:
          name: delete-test
        secretData:
          KEY: "value"
      """
    Then the ClientSecret "delete-test" should have phase "Ready" within 30 seconds
    And a Secret "delete-test" should exist
    When I delete the ClientSecret "delete-test"
    Then the ClientSecret "delete-test" should not exist within 30 seconds
    And the mock provider should have received at least 1 delete key calls within 30 seconds

  Scenario: Validation failure does not retry
    When I create a ClientSecret:
      """yaml
      apiVersion: mock.valet.ngl.cx/v1alpha1
      kind: ClientSecret
      metadata:
        name: validation-failure
      spec:
        secretRef:
          name: validation-failure
      """
    Then the ClientSecret "validation-failure" should have phase "Failed" within 30 seconds
    And the ClientSecret "validation-failure" status should contain message "secretData must contain at least one key"
    And the Secret "validation-failure" should not exist

  Scenario: Provider tracks provisioned credentials
    When I create a ClientSecret:
      """yaml
      apiVersion: mock.valet.ngl.cx/v1alpha1
      kind: ClientSecret
      metadata:
        name: tracking-test
      spec:
        secretRef:
          name: tracking-test
        secretData:
          KEY: "value"
      """
    Then the ClientSecret "tracking-test" should have phase "Ready" within 30 seconds
    And the mock provider should have received at least 1 provision calls

  Scenario: Expired credentials trigger rotation
    When I create a ClientSecret:
      """yaml
      apiVersion: mock.valet.ngl.cx/v1alpha1
      kind: ClientSecret
      metadata:
        name: rotation-test
      spec:
        secretRef:
          name: rotation-test
        secretData:
          KEY: "initial-value"
      """
    Then the ClientSecret "rotation-test" should have phase "Ready" within 30 seconds
    And the ClientSecret "rotation-test" should have 1 active keys
    And the mock provider should have received at least 1 provision calls
    When I expire the credentials for ClientSecret "rotation-test"
    Then the mock provider should have received at least 2 provision calls within 30 seconds

  Scenario: Spec update triggers re-provisioning
    When I create a ClientSecret "spec-update-test" with:
      """yaml
      spec:
        secretRef:
          name: spec-update-test
        secretData:
          KEY: "original-value"
      """
    Then the ClientSecret "spec-update-test" should have phase "Ready" within 30 seconds
    And the Secret "spec-update-test" should contain key "KEY" with value "original-value"
    When I update the ClientSecret "spec-update-test" with:
      """yaml
      spec:
        secretRef:
          name: spec-update-test
        secretData:
          KEY: "updated-value"
      """
    Then the Secret "spec-update-test" should contain key "KEY" with value "updated-value" within 30 seconds
    And the mock provider should have received at least 2 provision calls

  Scenario: DeleteKey failure keeps key in active list
    When I create a ClientSecret:
      """yaml
      apiVersion: mock.valet.ngl.cx/v1alpha1
      kind: ClientSecret
      metadata:
        name: delete-key-failure
      spec:
        secretRef:
          name: delete-key-failure
        shouldFailDeleteKey: true
        secretData:
          KEY: "value"
      """
    Then the ClientSecret "delete-key-failure" should have phase "Ready" within 30 seconds
    And the ClientSecret "delete-key-failure" should have 1 active keys
    When I expire the credentials for ClientSecret "delete-key-failure"
    Then the mock provider should have received at least 1 delete key calls within 30 seconds
    # Key stays in list because deletion failed
    And the ClientSecret "delete-key-failure" should have at least 1 active keys within 30 seconds
    And the mock provider should have received at least 2 provision calls
