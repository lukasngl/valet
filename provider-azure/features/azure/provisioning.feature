@azure
Feature: Azure AD Secret Provisioning
  As an operator
  I want to provision Azure AD client secrets
  So that applications can authenticate with Azure services

  Background:
    Given Azure credentials are configured

  Scenario: Provision credentials for owned application
    When I provision credentials for app "${TEST_AZURE_OWNED_APP_OBJECT_ID}" with:
      """yaml
      validity: "1h"
      template:
        CLIENT_ID: "{{ .ClientID }}"
        CLIENT_SECRET: "{{ .ClientSecret }}"
      """
    Then the provisioning should succeed
    And the result should contain key "CLIENT_ID"
    And the result should contain key "CLIENT_SECRET"
    And the result should have a valid key ID

  Scenario: Template renders with correct values
    When I provision credentials for app "${TEST_AZURE_OWNED_APP_OBJECT_ID}" with:
      """yaml
      validity: "1h"
      template:
        CLIENT_ID: "{{ .ClientID }}"
        CLIENT_ID_LEN: "{{ .ClientID | len }}"
        CLIENT_SECRET_LEN: "{{ .ClientSecret | len }}"
      """
    Then the provisioning should succeed
    And the result key "CLIENT_ID" should match pattern "^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$"
    And the result key "CLIENT_ID_LEN" should equal "36"
    And the result key "CLIENT_SECRET_LEN" should be a number greater than 0

  Scenario: Fail to provision for non-owned application
    When I provision credentials for app "${TEST_AZURE_OTHER_APP_OBJECT_ID}" with:
      """yaml
      validity: "1h"
      template:
        CLIENT_SECRET: "{{ .ClientSecret }}"
      """
    Then the provisioning should fail with "privileges"

  Scenario: Fail to provision for non-existent application
    When I provision credentials for app "00000000-0000-0000-0000-000000000000" with:
      """yaml
      validity: "1h"
      template:
        CLIENT_SECRET: "{{ .ClientSecret }}"
      """
    Then the provisioning should fail with "does not exist"
