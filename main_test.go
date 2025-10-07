package main

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/config"
	qc "github.com/bevelwork/quick_color"
)

// TestMain runs before all tests
func TestMain(m *testing.M) {
	// Set a fake AWS region to avoid default region issues
	os.Setenv("AWS_REGION", "us-east-1")
	os.Unsetenv("AWS_ACCESS_KEY_ID")
	os.Unsetenv("AWS_SECRET_ACCESS_KEY")
	os.Unsetenv("AWS_SESSION_TOKEN")
	os.Unsetenv("AWS_PROFILE")

	// Run tests
	code := m.Run()
	os.Exit(code)
}

// TestVersionFlag tests that the version flag works without AWS credentials
func TestVersionFlag(t *testing.T) {
	// This test verifies the version flag works without AWS setup
	// We can't easily test the full main() function, but we can test the version resolution
	version := resolveVersion()
	if version == "" {
		t.Error("Version should not be empty")
	}

	// Version should contain at least a number
	result := strings.Split(version, ".")
	major, minor, date := result[0], result[1], result[2]
	_, err := strconv.Atoi(major)
	if err != nil {
		t.Errorf("Version should contain a major version number, got: %s", version)
	}
	_, err = strconv.Atoi(minor)
	if err != nil {
		t.Errorf("Version should contain a minor version number, got: %s", version)
	}
	if date == "" {
		t.Errorf("Version should contain a date, got: %s", version)
	}
}

// TestQuickTagCreatedNameDetection tests the isQuickTagCreatedName function
func TestQuickTagCreatedNameDetection(t *testing.T) {
	tests := []struct {
		name         string
		resourceType string
		expected     bool
	}{
		// Instance tests
		{"instance-ami-12345678", "instance", true},
		{"unknown-instance", "instance", true},
		{"my-custom-instance", "instance", false},
		{"", "instance", false}, // Empty name is not considered quick-tag created for instances

		// Volume tests
		{"unattached", "volume", true},
		{"unattached-/dev/xvda1", "volume", true},
		{"unknown-volume", "volume", true},
		{"my-custom-volume", "volume", false},

		// ENI tests
		{"unattached-eni", "eni", true},
		{"service-123-eni", "eni", true},
		{"attached-456-eni", "eni", true},
		{"lambda-789-eni", "eni", true},
		{"rds-abc-eni", "eni", true},
		{"elasticache-def-eni", "eni", true},
		{"elb-ghi-eni", "eni", true},
		{"nat-jkl-eni", "eni", true},
		{"rds-eni-attach-123", "eni", true},
		{"elasticache-eni-attach-456", "eni", true},
		{"elb-eni-attach-789", "eni", true},
		{"lambda-eni-attach-abc", "eni", true},
		{"nat-eni-attach-def", "eni", true},
		{"service-eni-attach-ghi", "eni", true},
		{"rds-ela-attach-123", "eni", true},
		{"elasticache-ela-attach-456", "eni", true},
		{"elb-ela-attach-789", "eni", true},
		{"lambda-ela-attach-abc", "eni", true},
		{"nat-ela-attach-def", "eni", true},
		{"service-ela-attach-ghi", "eni", true},
		{"eni-1234567890123456789", "eni", true}, // 21 character ENI ID
		{"", "eni", true},                        // Empty name
		{"Network interface", "eni", true},
		{"primary network interface", "eni", true},
		{"my-custom-eni", "eni", false},
	}

	for _, test := range tests {
		result := isQuickTagCreatedName(test.name, test.resourceType)
		if result != test.expected {
			t.Errorf("isQuickTagCreatedName(%q, %q) = %v, expected %v", test.name, test.resourceType, result, test.expected)
		}
	}
}

// TestQuickTagNameStillValid tests the isQuickTagNameStillValid function
func TestQuickTagNameStillValid(t *testing.T) {
	tests := []struct {
		name         string
		resourceType string
		currentState string
		extraInfo    string
		expected     bool
	}{
		// Test non-quick-tag names (should always be valid)
		{"my-custom-instance", "instance", "running", "ami-123", true},
		{"my-custom-volume", "volume", "in-use", "/dev/xvda1", true},
		{"my-custom-eni", "eni", "in-use", "attached-to-i-123", true},

		// Test quick-tag created names that are still valid
		{"instance-ami-12345678", "instance", "running", "ami-12345678", true},
		{"unattached", "volume", "available", "unattached", true},
		{"unattached-/dev/xvda1", "volume", "available", "unattached", true},
		{"unattached-eni", "eni", "available", "unattached", true},
		{"service-123-eni", "eni", "in-use", "attached-to-i-123", true},

		// Test quick-tag created names that are no longer valid
		{"unattached", "volume", "in-use", "/dev/xvda1", false},            // Volume is now attached but name says unattached
		{"unattached-/dev/xvda1", "volume", "in-use", "/dev/xvda1", false}, // Volume is now attached but name says unattached
		{"unattached-eni", "eni", "in-use", "attached-to-i-123", false},    // ENI is now attached but name says unattached
	}

	for _, test := range tests {
		result := isQuickTagNameStillValid(test.name, test.resourceType, test.currentState, test.extraInfo)
		if result != test.expected {
			t.Errorf("isQuickTagNameStillValid(%q, %q, %q, %q) = %v, expected %v",
				test.name, test.resourceType, test.currentState, test.extraInfo, result, test.expected)
		}
	}
}

// TestGenericNameDetection tests the isGenericName function (backward compatibility)
func TestGenericNameDetection(t *testing.T) {
	tests := []struct {
		name         string
		resourceType string
		expected     bool
	}{
		// Instance tests
		{"instance-ami-12345678", "instance", true},
		{"unknown-instance", "instance", true},
		{"my-web-server", "instance", false},
		{"", "instance", false},

		// Volume tests
		{"unattached", "volume", true},
		{"unknown-/dev/sda1", "volume", true},
		{"web-server-/dev/xvda1", "volume", false},
		{"", "volume", false},

		// ENI tests
		{"unattached-eni", "eni", true},
		{"service-12345678-eni", "eni", true},
		{"attached-12345678-eni", "eni", true},
		{"lambda-12345678-eni", "eni", true},
		{"rds-12345678-eni", "eni", true},
		{"elasticache-12345678-eni", "eni", true},
		{"elb-12345678-eni", "eni", true},
		{"nat-12345678-eni", "eni", true},
		// Service attachment patterns (the ones that were causing issues)
		{"rds-eni-attach-04a07f99755b3d497-eni", "eni", true},
		{"elasticache-eni-attach-0829c25cfb70c6d8f-eni", "eni", true},
		{"elb-eni-attach-0ae1a06f8094ecc2f-eni", "eni", true},
		{"lambda-eni-attach-1234567890abcdef-eni", "eni", true},
		{"nat-eni-attach-1234567890abcdef-eni", "eni", true},
		{"service-eni-attach-1234567890abcdef-eni", "eni", true},
		// Alternative service attachment patterns (ela-attach-)
		{"rds-ela-attach-04a07f99755b3d497-eni", "eni", true},
		{"elasticache-ela-attach-0829c25cfb70c6d8f-eni", "eni", true},
		{"elb-ela-attach-0ae1a06f8094ecc2f-eni", "eni", true},
		{"lambda-ela-attach-1234567890abcdef-eni", "eni", true},
		{"nat-ela-attach-01cacbdcc2dd3038b-eni", "eni", true}, // The actual pattern from terminal
		{"service-ela-attach-1234567890abcdef-eni", "eni", true},
		// Generic service-attach pattern
		{"some-service-attach-1234567890abcdef-eni", "eni", true},
		{"eni-1234567890abcdef0", "eni", true}, // Just ENI ID
		{"Network interface for instance", "eni", true},
		{"primary network interface", "eni", true},
		{"web-server-eni", "eni", false},
		{"", "eni", true}, // Empty name should be considered generic
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isGenericName(tt.name, tt.resourceType)
			if result != tt.expected {
				t.Errorf("isGenericName(%q, %q) = %v, want %v", tt.name, tt.resourceType, result, tt.expected)
			}
		})
	}
}

// TestColorFunctions tests the color utility functions
func TestColorFunctions(t *testing.T) {
	// Test color function
	colored := color("test", qc.ColorRed)
	if colored == "test" {
		t.Error("Color function should add color codes")
	}

	// Test colorBold function
	boldColored := colorBold("test", qc.ColorRed)
	if boldColored == "test" {
		t.Error("ColorBold function should add color and bold codes")
	}

	// Test that both functions include the reset code
	if !contains(colored, qc.ColorReset) {
		t.Error("Color function should include reset code")
	}

	if !contains(boldColored, qc.ColorReset) {
		t.Error("ColorBold function should include reset code")
	}
}

// TestTagDisplayColors tests the color styling for tag displays
func TestTagDisplayColors(t *testing.T) {
	// Test untagged display (should be yellow)
	untaggedDisplay := color("untagged", qc.ColorYellow)
	expectedUntagged := qc.ColorYellow + "untagged" + qc.ColorReset
	if untaggedDisplay != expectedUntagged {
		t.Errorf("untagged display = %q, expected %q", untaggedDisplay, expectedUntagged)
	}

	// Test old tag display (should be red)
	oldTagDisplay := color("old-tag-name", qc.ColorRed)
	expectedOldTag := qc.ColorRed + "old-tag-name" + qc.ColorReset
	if oldTagDisplay != expectedOldTag {
		t.Errorf("old tag display = %q, expected %q", oldTagDisplay, expectedOldTag)
	}

	// Test new tag display (should be green)
	newTagDisplay := color("new-tag-name", qc.ColorGreen)
	expectedNewTag := qc.ColorGreen + "new-tag-name" + qc.ColorReset
	if newTagDisplay != expectedNewTag {
		t.Errorf("new tag display = %q, expected %q", newTagDisplay, expectedNewTag)
	}
}

// TestResourceStateColors tests the colorResourceState function
func TestResourceStateColors(t *testing.T) {
	tests := []struct {
		state    string
		expected string
	}{
		{"running", qc.ColorGreen},
		{"available", qc.ColorGreen},
		{"in-use", qc.ColorGreen},
		{"stopped", qc.ColorRed},
		{"stopping", qc.ColorRed},
		{"detaching", qc.ColorRed},
		{"pending", qc.ColorYellow},
		{"creating", qc.ColorYellow},
		{"attaching", qc.ColorYellow},
		{"terminated", qc.ColorRed},
		{"deleting", qc.ColorRed},
		{"detached", qc.ColorRed},
		{"unknown-state", qc.ColorWhite},
	}

	for _, tt := range tests {
		t.Run(tt.state, func(t *testing.T) {
			result := colorResourceState(tt.state)
			if result != tt.expected {
				t.Errorf("colorResourceState(%q) = %q, want %q", tt.state, result, tt.expected)
			}
		})
	}
}

// TestAWSConfigFailure tests that AWS config fails predictably without credentials
func TestAWSConfigFailure(t *testing.T) {
	// This test verifies that AWS config loading fails predictably
	// when no credentials are available
	ctx := context.Background()

	// Try to load AWS config - this might succeed if there are default credentials
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion("us-east-1"))
	if err != nil {
		// If it fails, the error should be related to credentials
		if !contains(err.Error(), "credential") && !contains(err.Error(), "NoCredentialProviders") {
			t.Errorf("Expected credential-related error, got: %v", err)
		}
		return
	}

	// If config loaded successfully, that's also fine - it means there are default credentials
	// We can't easily test the actual AWS API calls without mocking, so we'll just verify
	// the config was created successfully
	if cfg.Region == "" {
		t.Error("Config region should be set if no error occurred")
	}

	t.Log("AWS config loaded successfully - default credentials are available")
}

// TestHistoryFunctions tests the history management functions
func TestHistoryFunctions(t *testing.T) {
	// Test getHistoryFilePath
	path := getHistoryFilePath()
	if path == "" {
		t.Error("History file path should not be empty")
	}
	if !strings.Contains(path, ".quick-tag.yml") {
		t.Error("History file path should contain .quick-tag.yml")
	}

	// Clean up any existing history file for clean testing
	os.Remove(path)

	// Test loading non-existent history (should return empty history)
	history, err := loadHistory()
	if err != nil {
		t.Errorf("Loading non-existent history should not error: %v", err)
	}
	if history == nil {
		t.Error("History should not be nil")
	}
	if len(history.Actions) != 0 {
		t.Error("Non-existent history should have empty actions")
	}

	// Test adding to history
	err = addToHistory("123456789012", "i-1234567890abcdef0", "old-name", "new-name", "run-test123")
	if err != nil {
		t.Errorf("Adding to history should not error: %v", err)
	}

	// Test loading history after adding
	history, err = loadHistory()
	if err != nil {
		t.Errorf("Loading history after adding should not error: %v", err)
	}
	if len(history.Actions) != 1 {
		t.Errorf("History should have 1 action, got %d", len(history.Actions))
	}

	action := history.Actions[0]
	if action.Account != "123456789012" {
		t.Errorf("Expected account 123456789012, got %s", action.Account)
	}
	if action.Resource != "i-1234567890abcdef0" {
		t.Errorf("Expected resource i-1234567890abcdef0, got %s", action.Resource)
	}
	if action.OldValue != "old-name" {
		t.Errorf("Expected old value 'old-name', got %s", action.OldValue)
	}
	if action.NewValue != "new-name" {
		t.Errorf("Expected new value 'new-name', got %s", action.NewValue)
	}
	if action.Timestamp == "" {
		t.Error("Timestamp should not be empty")
	}
	if action.RunID != "run-test123" {
		t.Errorf("Expected RunID 'run-test123', got %s", action.RunID)
	}
}

// TestGenerateRunID tests the run ID generation function
func TestGenerateRunID(t *testing.T) {
	runID1 := generateRunID()
	runID2 := generateRunID()

	// Run IDs should not be empty
	if runID1 == "" {
		t.Error("Run ID should not be empty")
	}
	if runID2 == "" {
		t.Error("Run ID should not be empty")
	}

	// Run IDs should be different
	if runID1 == runID2 {
		t.Error("Generated run IDs should be unique")
	}

	// Run IDs should start with "run-"
	if !strings.HasPrefix(runID1, "run-") {
		t.Errorf("Run ID should start with 'run-', got: %s", runID1)
	}
	if !strings.HasPrefix(runID2, "run-") {
		t.Errorf("Run ID should start with 'run-', got: %s", runID2)
	}
}

// TestUndoFunctionality tests the undo functionality
func TestUndoFunctionality(t *testing.T) {
	// Clean up any existing history file for clean testing
	path := getHistoryFilePath()
	os.Remove(path)

	// Test undo with no history
	err := undoLastRun()
	if err == nil {
		t.Error("Undo should fail with no history")
	}
	if !strings.Contains(err.Error(), "no tagging history found") {
		t.Errorf("Expected 'no tagging history found' error, got: %v", err)
	}

	// Add some test history
	err = addToHistory("123456789012", "i-1234567890abcdef0", "old-name-1", "new-name-1", "run-test1")
	if err != nil {
		t.Errorf("Adding to history should not error: %v", err)
	}
	err = addToHistory("123456789012", "i-0987654321fedcba0", "old-name-2", "new-name-2", "run-test1")
	if err != nil {
		t.Errorf("Adding to history should not error: %v", err)
	}
	err = addToHistory("123456789012", "i-1111111111111111", "old-name-3", "new-name-3", "run-test2")
	if err != nil {
		t.Errorf("Adding to history should not error: %v", err)
	}

	// Load history and verify structure
	history, err := loadHistory()
	if err != nil {
		t.Errorf("Loading history should not error: %v", err)
	}
	if len(history.Actions) != 3 {
		t.Errorf("Expected 3 actions, got %d", len(history.Actions))
	}

	// Verify all actions are not undone initially
	for _, action := range history.Actions {
		if action.Undone {
			t.Error("Actions should not be marked as undone initially")
		}
	}

	// Test finding the last undone run
	lastRunID := ""
	for i := len(history.Actions) - 1; i >= 0; i-- {
		action := history.Actions[i]
		if !action.Undone {
			lastRunID = action.RunID
			break
		}
	}
	if lastRunID != "run-test2" {
		t.Errorf("Expected last run ID to be 'run-test2', got %s", lastRunID)
	}

	// Test marking actions as undone
	for i := range history.Actions {
		if history.Actions[i].RunID == "run-test1" {
			history.Actions[i].Undone = true
		}
	}

	// Save and reload to test persistence
	err = saveHistory(history)
	if err != nil {
		t.Errorf("Saving history should not error: %v", err)
	}

	history, err = loadHistory()
	if err != nil {
		t.Errorf("Loading history should not error: %v", err)
	}

	// Verify undone status persisted
	undoneCount := 0
	for _, action := range history.Actions {
		if action.Undone {
			undoneCount++
		}
	}
	if undoneCount != 2 {
		t.Errorf("Expected 2 undone actions, got %d", undoneCount)
	}
}

// TestUndoneFieldConsistency tests that the Undone field is always present in YAML output
func TestUndoneFieldConsistency(t *testing.T) {
	// Clean up any existing history file for clean testing
	path := getHistoryFilePath()
	os.Remove(path)

	// Add a test entry
	err := addToHistory("123456789012", "i-1234567890abcdef0", "old-name", "new-name", "run-test123")
	if err != nil {
		t.Errorf("Adding to history should not error: %v", err)
	}

	// Load the history and check the YAML output
	history, err := loadHistory()
	if err != nil {
		t.Errorf("Loading history should not error: %v", err)
	}

	if len(history.Actions) != 1 {
		t.Errorf("Expected 1 action, got %d", len(history.Actions))
	}

	action := history.Actions[0]
	if action.Undone != false {
		t.Errorf("Expected Undone to be false by default, got %v", action.Undone)
	}

	// Save the history and check that Undone field is present in YAML
	err = saveHistory(history)
	if err != nil {
		t.Errorf("Saving history should not error: %v", err)
	}

	// Read the raw YAML file to verify the field is present
	data, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("Reading history file should not error: %v", err)
	}

	yamlContent := string(data)
	if !strings.Contains(yamlContent, "Undone: false") {
		t.Errorf("YAML should contain 'Undone: false', got: %s", yamlContent)
	}

	// Test that setting Undone to true is preserved
	history.Actions[0].Undone = true
	err = saveHistory(history)
	if err != nil {
		t.Errorf("Saving history should not error: %v", err)
	}

	// Read again to verify true value is preserved
	data, err = os.ReadFile(path)
	if err != nil {
		t.Errorf("Reading history file should not error: %v", err)
	}

	yamlContent = string(data)
	if !strings.Contains(yamlContent, "Undone: true") {
		t.Errorf("YAML should contain 'Undone: true', got: %s", yamlContent)
	}
}

func TestExtractELBName(t *testing.T) {
	tests := []struct {
		description string
		expected    string
	}{
		{
			description: "ELB app/canvas-lb-sbx/35b9ec36d721abfe",
			expected:    "canvas-lb-sbx",
		},
		{
			description: "ELB net/my-lb/1234567890abcdef",
			expected:    "my-lb",
		},
		{
			description: "ELB app/production-api/abcdef1234567890",
			expected:    "production-api",
		},
		{
			description: "ELB net/test-lb-123/9876543210fedcba",
			expected:    "test-lb-123",
		},
		{
			description: "Not an ELB description",
			expected:    "",
		},
		{
			description: "ELB invalid-format",
			expected:    "",
		},
		{
			description: "",
			expected:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			result := extractELBName(tt.description)
			if result != tt.expected {
				t.Errorf("extractELBName(%q) = %q, want %q", tt.description, result, tt.expected)
			}
		})
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		(len(s) > len(substr) &&
			(s[:len(substr)] == substr ||
				s[len(s)-len(substr):] == substr ||
				containsInMiddle(s, substr))))
}

// Helper function to check if substr is contained in the middle of s
func containsInMiddle(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
