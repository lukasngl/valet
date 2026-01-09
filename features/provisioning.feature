Feature: Secret Provisioning
  As a platform operator
  I want to provision secrets from external providers
  So that applications can securely access credentials

  Background:
    Given a Kubernetes cluster is running
    And the CRDs are installed
    And the operator is running

  Scenario: Provision a secret successfully
    When I create a ClientSecret:
      """yaml
      apiVersion: secret-manager.ngl.cx/v1alpha1
      kind: ClientSecret
      metadata:
        name: test-secret
      spec:
        provider: mock
        secretRef:
          name: test-secret
        config:
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
      apiVersion: secret-manager.ngl.cx/v1alpha1
      kind: ClientSecret
      metadata:
        name: failing-secret
      spec:
        provider: mock
        secretRef:
          name: failing-secret
        config:
          shouldFail: true
          failureMessage: "simulated provider error"
          secretData:
            KEY: "value"
      """
    Then the ClientSecret "failing-secret" should have phase "Failed" within 30 seconds
