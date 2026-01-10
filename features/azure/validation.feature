@azure
Feature: Azure AD Config Validation
  As an operator
  I want config validation to catch errors early
  So that invalid configurations fail fast

  Background:
    Given Azure credentials are configured

  Scenario: Reject config with missing objectId
    When I validate config:
      """yaml
      template:
        SECRET: "{{ .ClientSecret }}"
      """
    Then validation should fail with "objectId"

  Scenario: Reject config with missing template
    When I validate config:
      """yaml
      objectId: "${TEST_AZURE_OWNED_APP_OBJECT_ID}"
      """
    Then validation should fail with "template"

  Scenario: Reject config with invalid validity format
    When I validate config:
      """yaml
      objectId: "${TEST_AZURE_OWNED_APP_OBJECT_ID}"
      validity: "invalid"
      template:
        SECRET: "{{ .ClientSecret }}"
      """
    Then validation should fail with "invalid validity duration"

  Scenario: Reject config with invalid template syntax
    When I validate config:
      """yaml
      objectId: "${TEST_AZURE_OWNED_APP_OBJECT_ID}"
      template:
        SECRET: "{{ .Invalid"
      """
    Then validation should fail with "template"

  Scenario: Accept valid config
    When I validate config:
      """yaml
      objectId: "${TEST_AZURE_OWNED_APP_OBJECT_ID}"
      validity: "1h"
      template:
        CLIENT_ID: "{{ .ClientID }}"
        CLIENT_SECRET: "{{ .ClientSecret }}"
      """
    Then validation should succeed
