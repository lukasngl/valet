Feature: Azure Secret Provisioning
  As a platform operator
  I want the Azure provider to provision and manage secrets
  So that applications can authenticate with Azure services

  Background:
    Given a Kubernetes cluster is running
    And the CRDs are installed
    And the operator is running

  Scenario: Provision a secret successfully
    When I create a ClientSecret "test-secret" with:
      """yaml
      spec:
        secretRef:
          name: test-secret
        objectId: "$TEST_AZURE_OBJECT_ID"
        template:
          CLIENT_ID: "{{ .ClientID }}"
          CLIENT_SECRET: "{{ .ClientSecret }}"
      """
    Then the ClientSecret "test-secret" should have phase "Ready" within 60 seconds
    And a Secret "test-secret" should exist
    And the Secret "test-secret" should contain key "CLIENT_ID"
    And the Secret "test-secret" should contain key "CLIENT_SECRET"

  Scenario: Invalid template syntax is rejected
    When I create a ClientSecret "bad-template" with:
      """yaml
      spec:
        secretRef:
          name: bad-template
        objectId: "$TEST_AZURE_OBJECT_ID"
        template:
          SECRET: "{{ .Invalid"
      """
    Then the ClientSecret "bad-template" should have phase "Failed" within 60 seconds
    And the ClientSecret "bad-template" status should contain message "template"
    And the Secret "bad-template" should not exist

  Scenario: Delete ClientSecret cleans up resources
    When I create a ClientSecret "delete-test" with:
      """yaml
      spec:
        secretRef:
          name: delete-test
        objectId: "$TEST_AZURE_OBJECT_ID"
        template:
          CLIENT_SECRET: "{{ .ClientSecret }}"
      """
    Then the ClientSecret "delete-test" should have phase "Ready" within 60 seconds
    And a Secret "delete-test" should exist
    When I delete the ClientSecret "delete-test"
    Then the ClientSecret "delete-test" should not exist within 60 seconds

  Scenario: Expired credentials are rotated
    When I create a ClientSecret "rotation-test" with:
      """yaml
      spec:
        secretRef:
          name: rotation-test
        objectId: "$TEST_AZURE_OBJECT_ID"
        template:
          CLIENT_SECRET: "{{ .ClientSecret }}"
      """
    Then the ClientSecret "rotation-test" should have phase "Ready" within 60 seconds
    And the ClientSecret "rotation-test" should have 1 active keys
    When I expire the credentials for ClientSecret "rotation-test"
    Then the ClientSecret "rotation-test" should have phase "Ready" within 60 seconds

  Scenario: Spec update triggers re-provisioning
    When I create a ClientSecret "spec-update" with:
      """yaml
      spec:
        secretRef:
          name: spec-update
        objectId: "$TEST_AZURE_OBJECT_ID"
        template:
          CLIENT_SECRET: "{{ .ClientSecret }}"
      """
    Then the ClientSecret "spec-update" should have phase "Ready" within 60 seconds
    And the Secret "spec-update" should contain key "CLIENT_SECRET"
    When I update the ClientSecret "spec-update" with:
      """yaml
      spec:
        secretRef:
          name: spec-update
        objectId: "$TEST_AZURE_OBJECT_ID"
        template:
          CLIENT_SECRET: "{{ .ClientSecret }}"
          CLIENT_ID: "{{ .ClientID }}"
      """
    Then the Secret "spec-update" should contain key "CLIENT_ID" within 60 seconds

  Scenario: Missing secretRef is rejected
    When I try to create a ClientSecret "no-ref" with:
      """yaml
      spec:
        objectId: "$TEST_AZURE_OBJECT_ID"
        template:
          CLIENT_SECRET: "{{ .ClientSecret }}"
      """
    Then the operation should have failed with "secretRef.name"

  Scenario: Secret is owned by the ClientSecret
    When I create a ClientSecret "ownership-test" with:
      """yaml
      spec:
        secretRef:
          name: ownership-test
        objectId: "$TEST_AZURE_OBJECT_ID"
        template:
          CLIENT_SECRET: "{{ .ClientSecret }}"
      """
    Then the ClientSecret "ownership-test" should have phase "Ready" within 60 seconds
    And the Secret "ownership-test" should be owned by ClientSecret "ownership-test"

  Scenario: Re-creating a deleted ClientSecret provisions fresh credentials
    When I create a ClientSecret "recreate-test" with:
      """yaml
      spec:
        secretRef:
          name: recreate-test
        objectId: "$TEST_AZURE_OBJECT_ID"
        template:
          CLIENT_SECRET: "{{ .ClientSecret }}"
      """
    Then the ClientSecret "recreate-test" should have phase "Ready" within 60 seconds
    And a Secret "recreate-test" should exist
    When I delete the ClientSecret "recreate-test"
    Then the ClientSecret "recreate-test" should not exist within 60 seconds
    When I create a ClientSecret "recreate-test" with:
      """yaml
      spec:
        secretRef:
          name: recreate-test
        objectId: "$TEST_AZURE_OBJECT_ID"
        template:
          CLIENT_SECRET: "{{ .ClientSecret }}"
      """
    Then the ClientSecret "recreate-test" should have phase "Ready" within 60 seconds
    And a Secret "recreate-test" should exist

  @mock
  Scenario: Template renders with correct values
    When I create a ClientSecret "value-check" with:
      """yaml
      spec:
        secretRef:
          name: value-check
        objectId: "$TEST_AZURE_OBJECT_ID"
        template:
          CLIENT_ID: "{{ .ClientID }}"
          CLIENT_SECRET: "{{ .ClientSecret }}"
      """
    Then the ClientSecret "value-check" should have phase "Ready" within 60 seconds
    And the Secret "value-check" should contain key "CLIENT_ID" with value "fake-app-id"
    And the Secret "value-check" should contain key "CLIENT_SECRET" with value "fake-secret-text"
