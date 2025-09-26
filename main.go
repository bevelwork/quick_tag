// Package main provides a command-line tool for quickly tagging AWS EC2 instances,
// EBS volumes, and ENIs that don't have Name tags. The tool scans all resources and
// provides an interactive interface for creating appropriate Name tags.

package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	versionpkg "github.com/bevelwork/quick_tag/version"
	"gopkg.in/yaml.v3"
)

// ANSI color codes for terminal output
const (
	ColorReset  = "\033[0m"
	ColorRed    = "\033[31m"
	ColorGreen  = "\033[32m"
	ColorYellow = "\033[33m"
	ColorBlue   = "\033[34m"
	ColorPurple = "\033[35m"
	ColorCyan   = "\033[36m"
	ColorWhite  = "\033[37m"
	ColorBold   = "\033[1m"
)

// ResourceInfo represents a resource that needs tagging
type ResourceInfo struct {
	ID            string // Resource ID
	Type          string // "instance", "volume", or "eni"
	Name          string // Current name (if any)
	SuggestedName string // Suggested name based on rules
	State         string // Resource state
	Extra         string // Additional info (AMI for instances, mount point for volumes, attachment info for ENIs)
}

// Config holds AWS clients and application configuration
type Config struct {
	EC2Client   *ec2.Client
	Region      string
	PrivateMode bool
}

// TagHistoryEntry represents a single tagging action in the history
type TagHistoryEntry struct {
	Account   string `yaml:"Account"`
	Resource  string `yaml:"Resource"`
	OldValue  string `yaml:"OldValue"`
	NewValue  string `yaml:"NewValue"`
	Timestamp string `yaml:"Timestamp"`
	RunID     string `yaml:"RunID"`
	Undone    bool   `yaml:"Undone"` // Track if this action has been undone (defaults to false)
}

// TagHistory represents the complete history of tagging actions
type TagHistory struct {
	Actions []TagHistoryEntry `yaml:"actions"`
}

// version is set at build time via ldflags
var version = ""

func main() {
	// Parse command line flags
	region := flag.String("region", "us-east-1", "AWS region to use")
	privateMode := flag.Bool("private", false, "Enable private mode (hide account information)")
	showVersion := flag.Bool("version", false, "Show version information")
	undoFlag := flag.Bool("undo", false, "Undo the last tagging run")
	flag.Parse()

	// Handle version flag
	if *showVersion {
		fmt.Println(resolveVersion())
		os.Exit(0)
	}

	// Handle undo flag
	if *undoFlag {
		if err := undoLastRun(); err != nil {
			log.Fatal(err)
		}
		return
	}

	// Generate a unique run ID for this execution
	runID := generateRunID()

	ctx := context.Background()

	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(*region))
	if err != nil {
		log.Fatal(err)
	}
	stsClient := sts.NewFromConfig(cfg)
	callerIdentity, err := stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		log.Fatal(fmt.Errorf("failed to authenticate with aws: %v", err))
	}
	printHeader(*privateMode, callerIdentity)

	// Create configuration with EC2 client
	config := &Config{
		EC2Client:   ec2.NewFromConfig(cfg),
		Region:      *region,
		PrivateMode: *privateMode,
	}

	// Step 1: Scan for untagged resources
	untaggedResources, err := showProgressWithResult("Scanning for untagged resources...", func() ([]*ResourceInfo, error) {
		return findUntaggedResources(ctx, config)
	})
	if err != nil {
		log.Fatal(err)
	}

	if len(untaggedResources) == 0 {
		fmt.Printf("%s All resources already have Name tags!\n", color("✅", ColorGreen))
		return
	}

	fmt.Printf("Found %d resources without Name tags:\n", len(untaggedResources))

	// Step 2: Display resources and allow selection
	selectedResources, autoApply := selectResources(untaggedResources)
	if len(selectedResources) == 0 {
		fmt.Println("No resources selected. Exiting.")
		return
	}

	// Step 3: Apply tags
	if err := applyTags(ctx, config, selectedResources, *callerIdentity.Account, runID, autoApply); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("\n%s Successfully completed tagging process!\n", color("✅", ColorGreen))
}

// findUntaggedResources scans for EC2 instances, EBS volumes, and ENIs without Name tags
func findUntaggedResources(ctx context.Context, config *Config) ([]*ResourceInfo, error) {
	var resources []*ResourceInfo

	// Find untagged instances
	instances, err := showProgressWithResult("Scanning EC2 instances...", func() ([]*ResourceInfo, error) {
		return findUntaggedInstances(ctx, config)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to find untagged instances: %v", err)
	}
	resources = append(resources, instances...)

	// Find untagged volumes
	volumes, err := showProgressWithResult("Scanning EBS volumes...", func() ([]*ResourceInfo, error) {
		return findUntaggedVolumes(ctx, config)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to find untagged volumes: %v", err)
	}
	resources = append(resources, volumes...)

	// Find untagged ENIs
	enis, err := showProgressWithResult("Scanning ENIs...", func() ([]*ResourceInfo, error) {
		return findUntaggedENIs(ctx, config)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to find untagged ENIs: %v", err)
	}
	resources = append(resources, enis...)

	// Sort by type, then by ID
	sort.Slice(resources, func(i, j int) bool {
		if resources[i].Type != resources[j].Type {
			return resources[i].Type < resources[j].Type
		}
		return resources[i].ID < resources[j].ID
	})

	return resources, nil
}

// findUntaggedInstances finds EC2 instances without Name tags
func findUntaggedInstances(ctx context.Context, config *Config) ([]*ResourceInfo, error) {
	var instances []*ResourceInfo

	paginator := ec2.NewDescribeInstancesPaginator(
		config.EC2Client, &ec2.DescribeInstancesInput{},
	)

	// Collect all AMI IDs to fetch their names in batch
	amiIDs := make(map[string]bool)

	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}

		for _, reservation := range output.Reservations {
			for _, instance := range reservation.Instances {
				// Skip terminated instances
				if instance.State.Name == types.InstanceStateNameTerminated {
					continue
				}

				// Check if instance has Name tag
				hasNameTag := false
				var currentName string
				for _, tag := range instance.Tags {
					if tag.Key != nil && *tag.Key == "Name" && tag.Value != nil {
						hasNameTag = true
						currentName = *tag.Value
						break
					}
				}

				// Include instances without Name tags OR with invalid quick-tag created names
				needsTagging := !hasNameTag || (hasNameTag && isQuickTagCreatedName(currentName, "instance") && !isQuickTagNameStillValid(currentName, "instance", string(instance.State.Name), *instance.ImageId))
				if needsTagging && instance.ImageId != nil {
					amiIDs[*instance.ImageId] = true

					instances = append(instances, &ResourceInfo{
						ID:            *instance.InstanceId,
						Type:          "instance",
						Name:          currentName,
						SuggestedName: "", // Will be filled after AMI lookup
						State:         string(instance.State.Name),
						Extra:         *instance.ImageId,
					})
				}
			}
		}
	}

	// Fetch AMI names in batch
	amiNames, err := getAMINames(ctx, config, amiIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to get AMI names: %v", err)
	}

	// Update suggested names with actual AMI names
	for _, instance := range instances {
		if amiName, exists := amiNames[instance.Extra]; exists {
			instance.SuggestedName = amiName
		} else {
			instance.SuggestedName = fmt.Sprintf("instance-%s", instance.Extra)
		}
	}

	return instances, nil
}

// findUntaggedVolumes finds EBS volumes without Name tags
func findUntaggedVolumes(ctx context.Context, config *Config) ([]*ResourceInfo, error) {
	var volumes []*ResourceInfo

	paginator := ec2.NewDescribeVolumesPaginator(
		config.EC2Client, &ec2.DescribeVolumesInput{},
	)

	// Collect all instance IDs to fetch their names in batch
	instanceIDs := make(map[string]bool)

	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}

		for _, volume := range output.Volumes {
			// Check if volume has Name tag
			hasNameTag := false
			var currentName string
			for _, tag := range volume.Tags {
				if tag.Key != nil && *tag.Key == "Name" && tag.Value != nil {
					hasNameTag = true
					currentName = *tag.Value
					break
				}
			}

			// Include volumes without Name tags OR with invalid quick-tag created names
			needsTagging := !hasNameTag || (hasNameTag && isQuickTagCreatedName(currentName, "volume") && !isQuickTagNameStillValid(currentName, "volume", string(volume.State), getVolumeMountPoint(volume)))
			if needsTagging {
				// Collect instance IDs for batch lookup
				for _, attachment := range volume.Attachments {
					if attachment.InstanceId != nil {
						instanceIDs[*attachment.InstanceId] = true
					}
				}

				volumes = append(volumes, &ResourceInfo{
					ID:            *volume.VolumeId,
					Type:          "volume",
					Name:          currentName,
					SuggestedName: "", // Will be filled after instance lookup
					State:         string(volume.State),
					Extra:         getVolumeMountPoint(volume),
				})
			}
		}
	}

	// Fetch instance names in batch
	instanceNames, err := getInstanceNames(ctx, config, instanceIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to get instance names: %v", err)
	}

	// Update suggested names with actual instance names
	for _, volume := range volumes {
		// Find the attached instance ID for this volume
		attachedInstanceID := getVolumeInstanceID(volume.ID, config.EC2Client, ctx)
		if attachedInstanceID != "" {
			if instanceName, exists := instanceNames[attachedInstanceID]; exists {
				volume.SuggestedName = fmt.Sprintf("%s(%s) %s", attachedInstanceID, instanceName, volume.Extra)
			} else {
				volume.SuggestedName = fmt.Sprintf("%s %s", attachedInstanceID, volume.Extra)
			}
		} else {
			// For unattached volumes, just use "unattached" without duplicating
			volume.SuggestedName = "unattached"
		}
	}

	return volumes, nil
}

// findUntaggedENIs finds ENIs without Name tags
func findUntaggedENIs(ctx context.Context, config *Config) ([]*ResourceInfo, error) {
	paginator := ec2.NewDescribeNetworkInterfacesPaginator(
		config.EC2Client, &ec2.DescribeNetworkInterfacesInput{},
	)

	// Collect all attachment IDs for batch lookup
	attachmentIDs := make(map[string]bool)
	var eniList []*ResourceInfo

	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}

		for _, eni := range output.NetworkInterfaces {
			// Check if ENI has Name tag
			hasNameTag := false
			var currentName string
			for _, tag := range eni.TagSet {
				if tag.Key != nil && *tag.Key == "Name" && tag.Value != nil {
					hasNameTag = true
					currentName = *tag.Value
					break
				}
			}

			// Include ENIs without Name tags OR with invalid quick-tag created names
			needsTagging := !hasNameTag || (hasNameTag && isQuickTagCreatedName(currentName, "eni") && !isQuickTagNameStillValid(currentName, "eni", string(eni.Status), getENIAttachmentInfo(eni)))
			if needsTagging {
				// Collect attachment IDs for batch lookup (only for EC2 instances)
				if eni.Attachment != nil && eni.Attachment.InstanceId != nil {
					attachmentIDs[*eni.Attachment.InstanceId] = true
				}

				eniList = append(eniList, &ResourceInfo{
					ID:            *eni.NetworkInterfaceId,
					Type:          "eni",
					Name:          currentName,
					SuggestedName: "", // Will be filled after attachment lookup
					State:         string(eni.Status),
					Extra:         getENIAttachmentInfo(eni),
					// Store the original ENI for later processing
				})
			}
		}
	}

	// Fetch attachment names in batch (only for EC2 instances)
	attachmentNames, err := getAttachmentNames(ctx, config, attachmentIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to get attachment names: %v", err)
	}

	// Update suggested names with actual attachment names
	for _, eni := range eniList {
		// Check if this ENI is attached to an EC2 instance
		if strings.HasPrefix(eni.Extra, "attached-to-") {
			instanceID := strings.TrimPrefix(eni.Extra, "attached-to-")
			if attachmentName, exists := attachmentNames[instanceID]; exists {
				eni.SuggestedName = fmt.Sprintf("%s-eni", attachmentName)
				// Format the Extra field to show "ID (name)"
				eni.Extra = fmt.Sprintf("%s (%s)", instanceID, attachmentName)
			} else {
				eni.SuggestedName = fmt.Sprintf("%s-eni", instanceID)
				// Just show the instance ID if no name found
				eni.Extra = instanceID
			}
		} else if strings.HasPrefix(eni.Extra, "attached-") {
			// For service attachments (NAT, RDS, ElastiCache, ELB, Lambda, etc.), extract the type and ID
			parts := strings.SplitN(eni.Extra, "-", 3) // attached-type-id or attached-elb-name
			if len(parts) >= 3 {
				attachmentType := parts[1] // nat, rds, elasticache, elb, lambda, etc.
				attachmentID := parts[2]   // the actual attachment ID or name

				// Special handling for ELB with extracted name
				if attachmentType == "elb" {
					eni.SuggestedName = fmt.Sprintf("%s-eni", attachmentID)
					eni.Extra = fmt.Sprintf("elb-%s", attachmentID)
				} else {
					// For other service types, use the standard naming
					eni.SuggestedName = fmt.Sprintf("%s-%s-eni", attachmentType, attachmentID)
					eni.Extra = fmt.Sprintf("%s-attachment-%s", attachmentType, attachmentID)
				}
			} else {
				// Fallback for unexpected format
				attachmentID := strings.TrimPrefix(eni.Extra, "attached-")
				eni.SuggestedName = fmt.Sprintf("service-%s-eni", attachmentID)
				eni.Extra = fmt.Sprintf("service-attachment-%s", attachmentID)
			}
		} else {
			// Unattached ENI
			eni.SuggestedName = "unattached-eni"
			eni.Extra = "unattached"
		}
	}

	return eniList, nil
}

// getAMINames fetches AMI names for the given AMI IDs
func getAMINames(ctx context.Context, config *Config, amiIDs map[string]bool) (map[string]string, error) {
	if len(amiIDs) == 0 {
		return make(map[string]string), nil
	}

	// Convert map keys to slice
	var amiIDSlice []string
	for amiID := range amiIDs {
		amiIDSlice = append(amiIDSlice, amiID)
	}

	// Describe AMIs in batches (AWS limit is 200 per request)
	amiNames := make(map[string]string)
	batchSize := 200

	for i := 0; i < len(amiIDSlice); i += batchSize {
		end := min(i+batchSize, len(amiIDSlice))
		batch := amiIDSlice[i:end]

		output, err := config.EC2Client.DescribeImages(ctx, &ec2.DescribeImagesInput{
			ImageIds: batch,
		})
		if err != nil {
			return nil, err
		}

		for _, image := range output.Images {
			if image.ImageId != nil && image.Name != nil {
				amiNames[*image.ImageId] = *image.Name
			}
		}
	}

	return amiNames, nil
}

// getInstanceNames fetches instance names for the given instance IDs
func getInstanceNames(ctx context.Context, config *Config, instanceIDs map[string]bool) (map[string]string, error) {
	if len(instanceIDs) == 0 {
		return make(map[string]string), nil
	}

	// Convert map keys to slice
	var instanceIDSlice []string
	for instanceID := range instanceIDs {
		instanceIDSlice = append(instanceIDSlice, instanceID)
	}

	// Describe instances in batches (AWS limit is 1000 per request)
	instanceNames := make(map[string]string)
	batchSize := 1000

	for i := 0; i < len(instanceIDSlice); i += batchSize {
		end := min(i+batchSize, len(instanceIDSlice))
		batch := instanceIDSlice[i:end]

		output, err := config.EC2Client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
			InstanceIds: batch,
		})
		if err != nil {
			return nil, err
		}

		for _, reservation := range output.Reservations {
			for _, instance := range reservation.Instances {
				if instance.InstanceId != nil {
					// Look for Name tag
					for _, tag := range instance.Tags {
						if tag.Key != nil && *tag.Key == "Name" && tag.Value != nil {
							instanceNames[*instance.InstanceId] = *tag.Value
							break
						}
					}
					// If no Name tag found, use instance ID
					if _, exists := instanceNames[*instance.InstanceId]; !exists {
						instanceNames[*instance.InstanceId] = *instance.InstanceId
					}
				}
			}
		}
	}

	return instanceNames, nil
}

// getVolumeMountPoint extracts the mount point from volume attachments
func getVolumeMountPoint(volume types.Volume) string {
	if len(volume.Attachments) == 0 {
		return "unattached"
	}

	attachment := volume.Attachments[0]
	if attachment.Device != nil {
		return *attachment.Device
	}

	return "unknown"
}

// getVolumeInstanceID gets the instance ID for a volume
func getVolumeInstanceID(volumeID string, ec2Client *ec2.Client, ctx context.Context) string {
	output, err := ec2Client.DescribeVolumes(ctx, &ec2.DescribeVolumesInput{
		VolumeIds: []string{volumeID},
	})
	if err != nil || len(output.Volumes) == 0 {
		return ""
	}

	volume := output.Volumes[0]
	if len(volume.Attachments) == 0 {
		return ""
	}

	attachment := volume.Attachments[0]
	if attachment.InstanceId != nil {
		return *attachment.InstanceId
	}

	return ""
}

// extractELBName extracts the load balancer name from ENI description
func extractELBName(description string) string {
	// ELB descriptions typically look like: "ELB app/canvas-lb-sbx/35b9ec36d721abfe"
	// or "ELB net/my-lb/1234567890abcdef"
	// We want to extract the load balancer name (canvas-lb-sbx or my-lb)

	// Split by spaces and look for the ELB pattern
	parts := strings.Fields(description)
	for i, part := range parts {
		if strings.ToLower(part) == "elb" && i+1 < len(parts) {
			// The next part should be the ELB ARN or name
			elbPart := parts[i+1]
			// Split by '/' to get the name part
			elbParts := strings.Split(elbPart, "/")
			if len(elbParts) >= 2 {
				// Return the load balancer name (second part after the type)
				return elbParts[1]
			}
		}
	}
	return ""
}

// getENIAttachmentInfo extracts attachment information from ENI
func getENIAttachmentInfo(eni types.NetworkInterface) string {
	if eni.Attachment == nil {
		return "unattached"
	}

	// Handle EC2 instance attachments
	if eni.Attachment.InstanceId != nil {
		return fmt.Sprintf("attached-to-%s", *eni.Attachment.InstanceId)
	}

	// Handle service attachments (NAT, RDS, ElastiCache, ELB, Lambda, etc.)
	if eni.Attachment.AttachmentId != nil {
		// Try to determine the attachment type from the description
		attachmentType := "service"
		if eni.Description != nil {
			desc := strings.ToLower(*eni.Description)
			if strings.Contains(desc, "lambda") {
				attachmentType = "lambda"
			} else if strings.Contains(desc, "rds") {
				attachmentType = "rds"
			} else if strings.Contains(desc, "elasticache") || strings.Contains(desc, "cache") {
				attachmentType = "elasticache"
			} else if strings.Contains(desc, "elb") || strings.Contains(desc, "load balancer") {
				attachmentType = "elb"
				// For ELB, try to extract the load balancer name
				if elbName := extractELBName(*eni.Description); elbName != "" {
					return fmt.Sprintf("attached-elb-%s", elbName)
				}
			} else if strings.Contains(desc, "nat") {
				attachmentType = "nat"
			}
		}
		return fmt.Sprintf("attached-%s-%s", attachmentType, *eni.Attachment.AttachmentId)
	}

	return "attached-unknown"
}

// getAttachmentNames fetches names for attached resources (instances, etc.)
func getAttachmentNames(ctx context.Context, config *Config, attachmentIDs map[string]bool) (map[string]string, error) {
	if len(attachmentIDs) == 0 {
		return make(map[string]string), nil
	}

	// Convert map keys to slice
	var attachmentIDSlice []string
	for attachmentID := range attachmentIDs {
		attachmentIDSlice = append(attachmentIDSlice, attachmentID)
	}

	instanceNames := make(map[string]string)
	batchSize := 1000

	for i := 0; i < len(attachmentIDSlice); i += batchSize {
		end := min(i+batchSize, len(attachmentIDSlice))
		batch := attachmentIDSlice[i:end]

		output, err := config.EC2Client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
			InstanceIds: batch,
		})
		if err != nil {
			return nil, err
		}

		for _, reservation := range output.Reservations {
			for _, instance := range reservation.Instances {
				if instance.InstanceId != nil {
					// Look for Name tag
					for _, tag := range instance.Tags {
						if tag.Key != nil && *tag.Key == "Name" && tag.Value != nil {
							instanceNames[*instance.InstanceId] = *tag.Value
							break
						}
					}
					// If no Name tag found, use instance ID
					if _, exists := instanceNames[*instance.InstanceId]; !exists {
						instanceNames[*instance.InstanceId] = *instance.InstanceId
					}
				}
			}
		}
	}

	return instanceNames, nil
}

// Helper functions

// color wraps a string with the specified color code
func color(text, colorCode string) string {
	return colorCode + text + ColorReset
}

// Progress indicator functions
var throbberChars = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// showProgress runs a throbber animation while executing a function
func showProgress(message string, fn func() error) error {
	done := make(chan error, 1)

	// Start the operation in a goroutine
	go func() {
		done <- fn()
	}()

	// Show throbber while waiting
	i := 0
	for {
		select {
		case err := <-done:
			// Clear the line and return
			fmt.Printf("\r\033[K")
			return err
		default:
			// Show throbber
			fmt.Printf("\r%s %s", throbberChars[i%len(throbberChars)], message)
			i++
			// Small delay to make throbber visible
			time.Sleep(100 * time.Millisecond)
		}
	}
}

// showProgressWithResult runs a throbber animation while executing a function that returns a result
func showProgressWithResult[T any](message string, fn func() (T, error)) (T, error) {
	done := make(chan struct {
		result T
		err    error
	}, 1)

	// Start the operation in a goroutine
	go func() {
		result, err := fn()
		done <- struct {
			result T
			err    error
		}{result, err}
	}()

	// Show throbber while waiting
	i := 0
	for {
		select {
		case res := <-done:
			// Clear the line and return
			fmt.Printf("\r\033[K")
			return res.result, res.err
		default:
			// Show throbber
			fmt.Printf("\r%s %s", throbberChars[i%len(throbberChars)], message)
			i++
			// Small delay to make throbber visible
			time.Sleep(100 * time.Millisecond)
		}
	}
}

// startThrobber starts a spinner with a fixed message and returns a stop function.
// Safe to call from long-running operations where you cannot wrap the whole body in a closure.
func startThrobber(message string) (stop func()) {
	done := make(chan struct{})
	go func() {
		i := 0
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				fmt.Printf("\r\033[K")
				return
			case <-ticker.C:
				fmt.Printf("\r%s %s", throbberChars[i%len(throbberChars)], message)
				i++
			}
		}
	}()
	return func() { close(done) }
}

// colorBold wraps a string with the specified color code and bold formatting
func colorBold(text, colorCode string) string {
	return colorCode + ColorBold + text + ColorReset
}

// colorResourceState returns the appropriate color for resource state
func colorResourceState(state string) string {
	switch state {
	case "running", "available", "in-use":
		return ColorGreen
	case "stopped", "stopping", "detaching":
		return ColorRed
	case "pending", "creating", "attaching":
		return ColorYellow
	case "terminated", "deleting", "detached":
		return ColorRed
	default:
		return ColorWhite
	}
}

// printHeader prints the application header
func printHeader(privateMode bool, callerIdentity *sts.GetCallerIdentityOutput) {
	header := []string{
		color(strings.Repeat("-", 40), ColorBlue),
		"-- AWS Quick Tag --",
		color(strings.Repeat("-", 40), ColorBlue),
	}
	if !privateMode {
		header = append(header, fmt.Sprintf(
			"  Account: %s \n  User: %s",
			*callerIdentity.Account, *callerIdentity.Arn,
		))
		header = append(header, color(strings.Repeat("-", 40), ColorBlue))
	}

	fmt.Println(strings.Join(header, "\n"))
}

// resolveVersion returns the version string
func resolveVersion() string {
	if strings.TrimSpace(version) != "" {
		return version
	}
	if strings.TrimSpace(versionpkg.Full) != "" {
		return versionpkg.Full
	}
	return fmt.Sprintf("v%d.%d.%s", versionpkg.Major, versionpkg.Minor, "unknown")
}

// stringPtr returns a pointer to a string value
func stringPtr(s string) *string {
	return &s
}

// getHistoryFilePath returns the path to the history file
func getHistoryFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".quick-tag.yml")
}

// loadHistory loads the tagging history from the YAML file
func loadHistory() (*TagHistory, error) {
	historyPath := getHistoryFilePath()
	if historyPath == "" {
		return &TagHistory{Actions: []TagHistoryEntry{}}, nil
	}

	data, err := os.ReadFile(historyPath)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist, return empty history
			return &TagHistory{Actions: []TagHistoryEntry{}}, nil
		}
		return nil, err
	}

	var history TagHistory
	if err := yaml.Unmarshal(data, &history); err != nil {
		return nil, err
	}

	// Note: The Undone field will be false by default due to Go's zero value for bool
	// This ensures backward compatibility with existing history files that don't have this field

	return &history, nil
}

// saveHistory saves the tagging history to the YAML file
func saveHistory(history *TagHistory) error {
	historyPath := getHistoryFilePath()
	if historyPath == "" {
		return fmt.Errorf("unable to determine home directory")
	}

	data, err := yaml.Marshal(history)
	if err != nil {
		return err
	}

	// Ensure the directory exists
	dir := filepath.Dir(historyPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	return os.WriteFile(historyPath, data, 0644)
}

// generateRunID creates a unique identifier for this execution run
func generateRunID() string {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to timestamp-based ID if crypto/rand fails
		return fmt.Sprintf("run-%d", time.Now().Unix())
	}
	return fmt.Sprintf("run-%s", hex.EncodeToString(bytes))
}

// addToHistory adds a new tagging action to the history
func addToHistory(account, resource, oldValue, newValue, runID string) error {
	history, err := loadHistory()
	if err != nil {
		return err
	}

	entry := TagHistoryEntry{
		Account:   account,
		Resource:  resource,
		OldValue:  oldValue,
		NewValue:  newValue,
		Timestamp: time.Now().Format(time.RFC3339),
		RunID:     runID,
		Undone:    false,
	}

	history.Actions = append(history.Actions, entry)
	return saveHistory(history)
}

// undoLastRun finds the last run that hasn't been undone and reverts all its actions
func undoLastRun() error {
	history, err := loadHistory()
	if err != nil {
		return fmt.Errorf("failed to load history: %v", err)
	}

	if len(history.Actions) == 0 {
		return fmt.Errorf("no tagging history found")
	}

	// Find the last run ID that hasn't been undone
	var lastRunID string
	for i := len(history.Actions) - 1; i >= 0; i-- {
		action := history.Actions[i]
		if !action.Undone {
			lastRunID = action.RunID
			break
		}
	}

	if lastRunID == "" {
		return fmt.Errorf("no undone runs found")
	}

	// Find all actions for this run
	var actionsToUndo []TagHistoryEntry
	for _, action := range history.Actions {
		if action.RunID == lastRunID && !action.Undone {
			actionsToUndo = append(actionsToUndo, action)
		}
	}

	if len(actionsToUndo) == 0 {
		return fmt.Errorf("no actions found for run %s", lastRunID)
	}

	// Show what will be undone
	fmt.Printf("🔄 Undoing run %s (%d actions):\n", lastRunID, len(actionsToUndo))
	for _, action := range actionsToUndo {
		fmt.Printf("  %s: '%s' -> '%s'\n", action.Resource, action.NewValue, action.OldValue)
	}

	// Ask for confirmation
	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("Are you sure you want to undo these changes? (y/N): ")
	response, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read user input: %v", err)
	}

	response = strings.TrimSpace(strings.ToLower(response))
	if response != "y" && response != "yes" {
		fmt.Println("Undo cancelled.")
		return nil
	}

	// Initialize AWS client for undo operations
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("failed to load AWS config: %v", err)
	}
	ec2Client := ec2.NewFromConfig(cfg)

	// Perform the undo operations
	successCount := 0
	notFoundCount := 0
	errorCount := 0

	for _, action := range actionsToUndo {
		fmt.Printf("Reverting %s: '%s' -> '%s'...\n", action.Resource, action.NewValue, action.OldValue)

		// Create tags input - if OldValue is empty, we need to delete the Name tag
		var input *ec2.CreateTagsInput
		if action.OldValue == "" {
			// Delete the Name tag by setting it to empty (AWS will remove it)
			input = &ec2.CreateTagsInput{
				Resources: []string{action.Resource},
				Tags: []types.Tag{
					{
						Key:   stringPtr("Name"),
						Value: stringPtr(""),
					},
				},
			}
		} else {
			// Set the tag back to the old value
			input = &ec2.CreateTagsInput{
				Resources: []string{action.Resource},
				Tags: []types.Tag{
					{
						Key:   stringPtr("Name"),
						Value: stringPtr(action.OldValue),
					},
				},
			}
		}

		_, err := ec2Client.CreateTags(ctx, input)
		if err != nil {
			// Check if the error is because the resource doesn't exist
			if strings.Contains(err.Error(), "NotFound") || strings.Contains(err.Error(), "does not exist") {
				fmt.Printf("Info: Resource %s no longer exists (likely deleted) - skipping\n", action.Resource)
				notFoundCount++
			} else {
				fmt.Printf("Warning: Failed to revert %s: %v\n", action.Resource, err)
				errorCount++
			}
			continue
		}

		successCount++
	}

	// Mark all actions in this run as undone
	for i := range history.Actions {
		if history.Actions[i].RunID == lastRunID && !history.Actions[i].Undone {
			history.Actions[i].Undone = true
		}
	}

	// Save the updated history
	if err := saveHistory(history); err != nil {
		return fmt.Errorf("failed to save updated history: %v", err)
	}

	// Provide detailed feedback about the undo operation
	totalActions := len(actionsToUndo)
	if successCount == totalActions {
		fmt.Printf("✅ Successfully undone all %d actions from run %s\n", successCount, lastRunID)
	} else {
		fmt.Printf("✅ Undo completed for run %s:\n", lastRunID)
		fmt.Printf("   - Successfully reverted: %d actions\n", successCount)
		if notFoundCount > 0 {
			fmt.Printf("   - Resources no longer exist: %d actions (skipped)\n", notFoundCount)
		}
		if errorCount > 0 {
			fmt.Printf("   - Failed to revert: %d actions (see warnings above)\n", errorCount)
		}
	}

	// Mark all actions as undone regardless of success/failure
	// This prevents trying to undo the same run again
	fmt.Printf("📝 Marked all %d actions from run %s as undone in history\n", totalActions, lastRunID)
	return nil
}

// isQuickTagCreatedName checks if a name was created by quick-tag
func isQuickTagCreatedName(name, resourceType string) bool {
	switch resourceType {
	case "instance":
		// Check for quick-tag created instance names like "instance-ami-12345678"
		return strings.HasPrefix(name, "instance-ami-") ||
			strings.HasPrefix(name, "unknown-instance")
	case "volume":
		// Check for quick-tag created volume names like "unattached", "unattached-/dev/xvda1"
		return name == "unattached" ||
			strings.HasPrefix(name, "unattached-") ||
			strings.HasPrefix(name, "unknown-")
	case "eni":
		// Check for quick-tag created ENI names like "unattached-eni", "service-123-eni"
		return name == "unattached-eni" ||
			strings.HasPrefix(name, "service-") && strings.HasSuffix(name, "-eni") ||
			strings.HasPrefix(name, "attached-") && strings.HasSuffix(name, "-eni") ||
			strings.HasPrefix(name, "lambda-") && strings.HasSuffix(name, "-eni") ||
			strings.HasPrefix(name, "rds-") && strings.HasSuffix(name, "-eni") ||
			strings.HasPrefix(name, "elasticache-") && strings.HasSuffix(name, "-eni") ||
			strings.HasPrefix(name, "elb-") && strings.HasSuffix(name, "-eni") ||
			strings.HasPrefix(name, "nat-") && strings.HasSuffix(name, "-eni") ||
			// Check for service attachment patterns (rds-eni-attach-, elasticache-eni-attach-, etc.)
			strings.HasPrefix(name, "rds-eni-attach-") ||
			strings.HasPrefix(name, "elasticache-eni-attach-") ||
			strings.HasPrefix(name, "elb-eni-attach-") ||
			strings.HasPrefix(name, "lambda-eni-attach-") ||
			strings.HasPrefix(name, "nat-eni-attach-") ||
			strings.HasPrefix(name, "service-eni-attach-") ||
			// Check for alternative service attachment patterns (nat-ela-attach-, etc.)
			strings.HasPrefix(name, "rds-ela-attach-") ||
			strings.HasPrefix(name, "elasticache-ela-attach-") ||
			strings.HasPrefix(name, "elb-ela-attach-") ||
			strings.HasPrefix(name, "lambda-ela-attach-") ||
			strings.HasPrefix(name, "nat-ela-attach-") ||
			strings.HasPrefix(name, "service-ela-attach-") ||
			// Check for any service-attach pattern ending in -eni
			strings.Contains(name, "-attach-") && strings.HasSuffix(name, "-eni") ||
			// Check for AWS default patterns
			strings.HasPrefix(name, "eni-") && len(name) >= 21 && len(name) <= 25 || // Just the ENI ID itself (eni- + variable length ID)
			name == "" || // Empty name
			strings.HasPrefix(name, "Network interface") || // AWS default description-based names
			strings.Contains(name, "primary") && strings.Contains(name, "interface") // Primary network interface
	}
	return false
}

// isQuickTagNameStillValid checks if a quick-tag created name is still valid for the current resource state
func isQuickTagNameStillValid(name, resourceType, currentState, extraInfo string) bool {
	if !isQuickTagCreatedName(name, resourceType) {
		return true // Not a quick-tag created name, so it's valid
	}

	switch resourceType {
	case "instance":
		// For instances, quick-tag names are generally still valid unless the AMI changed
		// This is a simple check - in practice, you might want to verify the AMI matches
		return true
	case "volume":
		// For volumes, check if the attachment state matches the name
		if name == "unattached" {
			// Name says unattached, check if it's actually unattached
			return currentState == "available" || extraInfo == "unattached"
		} else if strings.HasPrefix(name, "unattached-") {
			// Name says unattached with mount point, check if it's actually unattached
			return currentState == "available" || extraInfo == "unattached"
		}
		// For attached volumes, the name should match the current attachment
		return true // For now, assume attached volume names are still valid
	case "eni":
		// For ENIs, check if the attachment state matches the name
		if name == "unattached-eni" {
			// Name says unattached, check if it's actually unattached
			return extraInfo == "unattached"
		} else if strings.HasPrefix(name, "unattached-") {
			// Name says unattached, check if it's actually unattached
			return extraInfo == "unattached"
		} else if strings.Contains(name, "-eni") {
			// For attached ENIs, check if the attachment info matches
			// This is a simplified check - in practice, you might want more detailed validation
			return extraInfo != "unattached"
		}
	}
	return true
}

// isGenericName checks if a name is a generic placeholder that should be updated
// This function is kept for backward compatibility but now delegates to isQuickTagCreatedName
func isGenericName(name, resourceType string) bool {
	return isQuickTagCreatedName(name, resourceType)
}

// selectResources displays resources and allows user to select which ones to tag
func selectResources(resources []*ResourceInfo) ([]*ResourceInfo, bool) {
	fmt.Printf("\n%s\n", color("Resources without Name tags:", ColorBlue))

	longestID := 0
	for _, resource := range resources {
		if len(resource.ID) > longestID {
			longestID = len(resource.ID)
		}
	}

	for i, resource := range resources {
		// Alternate row colors for better readability
		var rowColor string
		if i%2 == 0 {
			rowColor = ColorWhite
		} else {
			rowColor = ColorCyan
		}

		// Show current name (or "untagged" if empty) and suggested name with color styling
		var currentNameDisplay string
		if resource.Name == "" {
			currentNameDisplay = color("untagged", ColorYellow)
		} else {
			currentNameDisplay = color(resource.Name, ColorRed)
		}

		suggestedNameDisplay := color(resource.SuggestedName, ColorGreen)

		entry := fmt.Sprintf(
			"%3d. %-*s %s -> %s",
			i+1, longestID, resource.ID, currentNameDisplay, suggestedNameDisplay,
		)
		fmt.Println(color(entry, rowColor))
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("%s", color("Select resources to tag (comma-separated numbers, or 'all' for all). Enter for all resources: ", ColorYellow))
	input, err := reader.ReadString('\n')
	if err != nil {
		log.Fatal(err)
	}
	input = strings.TrimSpace(input)
	if input == "" {
		fmt.Printf("%s No input provided, selecting all resources for sequential tagging.\n", color("ℹ️", ColorCyan))
		return resources, false // false = prompt for each tag
	}

	if strings.ToLower(input) == "all" {
		fmt.Printf("%s Auto-applying all tags without individual confirmation.\n", color("ℹ️", ColorCyan))
		return resources, true // true = auto-apply all tags
	}

	// Parse comma-separated numbers
	var selected []*ResourceInfo
	parts := strings.Split(input, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if idx, err := strconv.Atoi(part); err == nil {
			if idx >= 1 && idx <= len(resources) {
				selected = append(selected, resources[idx-1])
			} else {
				fmt.Printf("%s Invalid selection: %d (valid range: 1-%d)\n", color("⚠️", ColorYellow), idx, len(resources))
			}
		} else {
			fmt.Printf("%s Invalid input: '%s' (expected number)\n", color("⚠️", ColorYellow), part)
		}
	}

	if len(selected) == 0 {
		fmt.Printf("%s No valid selections made.\n", color("ℹ️", ColorCyan))
	}

	return selected, false // false = prompt for each tag
}

// applyTags applies Name tags to the selected resources
func applyTags(ctx context.Context, config *Config, resources []*ResourceInfo, accountID, runID string, autoApply bool) error {
	successCount := 0

	for i, resource := range resources {
		// Show the resource to be tagged
		fmt.Printf("\n%s Tag %d of %d:\n", color("🏷️", ColorBlue), i+1, len(resources))
		fmt.Printf("  Resource: %s %s\n", resource.Type, resource.ID)

		// Display current name with color styling
		if resource.Name == "" {
			fmt.Printf("  Current: %s\n", color("untagged", ColorYellow))
		} else {
			fmt.Printf("  Current: %s\n", color(resource.Name, ColorRed))
		}

		// Display new name with color styling
		fmt.Printf("  New: %s\n", color(resource.SuggestedName, ColorGreen))

		// Prompt user to continue (unless auto-applying)
		if !autoApply {
			reader := bufio.NewReader(os.Stdin)
			fmt.Printf("%s Press Enter to apply this tag (or Ctrl+C to cancel): ", color("→", ColorYellow))
			_, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("failed to read user input: %v", err)
			}
		} else {
			fmt.Printf("%s Auto-applying tag...\n", color("→", ColorYellow))
		}

		// Apply the tag with progress indicator
		err := showProgress(fmt.Sprintf("Applying tag to %s %s...", resource.Type, resource.ID), func() error {
			input := &ec2.CreateTagsInput{
				Resources: []string{resource.ID},
				Tags: []types.Tag{
					{
						Key:   stringPtr("Name"),
						Value: stringPtr(resource.SuggestedName),
					},
				},
			}

			_, err := config.EC2Client.CreateTags(ctx, input)
			if err != nil {
				return fmt.Errorf("failed to tag %s %s: %v", resource.Type, resource.ID, err)
			}

			// Log the tagging action to history
			if err := addToHistory(accountID, resource.ID, resource.Name, resource.SuggestedName, runID); err != nil {
				// Don't fail the tagging operation if history logging fails, just log a warning
				fmt.Printf("Warning: Failed to log tagging action to history: %v\n", err)
			}

			return nil
		})

		if err != nil {
			// Stop on first failure
			fmt.Printf("%s Failed to apply tag: %v\n", color("❌", ColorRed), err)
			fmt.Printf("%s Stopping tagging process after %d successful applications.\n", color("⚠️", ColorYellow), successCount)
			return err
		}

		successCount++
		fmt.Printf("%s Successfully tagged %s %s\n", color("✅", ColorGreen), resource.Type, resource.ID)
	}

	return nil
}
