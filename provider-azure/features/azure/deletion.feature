@azure
Feature: Azure AD Key Deletion
  As an operator
  I want to delete provisioned keys
  So that expired credentials are cleaned up

  Background:
    Given Azure credentials are configured

  Scenario: Delete provisioned key
    Given I have provisioned credentials for app "${TEST_AZURE_OWNED_APP_OBJECT_ID}"
    When I delete the provisioned key
    Then the deletion should succeed

  Scenario: Delete non-existent key is idempotent
    When I delete key "00000000-0000-0000-0000-000000000000" for app "${TEST_AZURE_OWNED_APP_OBJECT_ID}"
    Then the deletion should succeed

  Scenario: Delete with invalid key ID format fails
    When I delete key "not-a-uuid" for app "${TEST_AZURE_OWNED_APP_OBJECT_ID}"
    Then the deletion should fail with "invalid key ID"

  Scenario: Delete with empty key ID is no-op
    When I delete key "" for app "${TEST_AZURE_OWNED_APP_OBJECT_ID}"
    Then the deletion should succeed
