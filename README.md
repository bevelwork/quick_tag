# Quick Tag

A command-line tool for quickly tagging AWS EC2 instances, EBS volumes, and ENIs that don't have Name tags. The tool scans all resources and provides an interactive interface for creating appropriate Name tags.

## Features

- **Automatic Resource Discovery**: Scans all EC2 instances, EBS volumes, and ENIs in your AWS account
- **Smart Naming**: 
  - Instances without names are named after their AMI
  - EBS volumes are named after their attached instance plus mount point
  - ENIs are named after their attached resource (e.g., "web-server-eni", "rds-12345678-eni")
- **Interactive Selection**: Choose which resources to tag with a simple numbered interface
- **Batch Operations**: Efficiently processes multiple resources at once
- **Color-coded Output**: Easy-to-read terminal interface with status colors
- **Action History**: Tracks all tagging actions in `~/.quick-tag.yml` for auditing and review
- **Undo Functionality**: Revert the last tagging run with `--undo` flag

## Installation

### From Source

```bash
git clone https://github.com/bevelwork/quick_tag.git
cd quick_tag
go build -o quick_tag .
```

### Binary Release

Download the latest binary from the releases page.

## Usage

### Basic Usage

```bash
./quick_tag
```

### Command Line Options

```bash
./quick_tag [options]

Options:
  -region string
        AWS region to use (default "us-east-1")
  -private
        Enable private mode (hide account information)
  -version
        Show version information
```

### Examples

```bash
# Use a specific region
./quick_tag -region us-west-2

# Hide account information
./quick_tag -private

# Check version
./quick_tag -version
```

## How It Works

1. **Scan**: The tool scans all EC2 instances, EBS volumes, and ENIs in your AWS account
2. **Identify**: Resources without `Name` tags are identified
3. **Suggest**: Appropriate names are suggested based on:
   - **Instances**: Named after their AMI (e.g., "amzn2-ami-hvm-2.0.20230920.0-x86_64-gp2")
   - **Volumes**: Named after their attached instance plus mount point (e.g., "web-server-/dev/xvda1")
   - **ENIs**: Named after their attached resource (e.g., "web-server-eni")
4. **Select**: You choose which resources to tag using a numbered interface
5. **Tag**: The selected resources are tagged with their suggested names

## Naming Rules

### EC2 Instances
- If an instance has no `Name` tag, it will be named after its AMI
- Example: `amzn2-ami-hvm-2.0.20230920.0-x86_64-gp2`

### EBS Volumes
- If a volume has no `Name` tag, it will be named after its attached instance plus mount point
- Example: `i-1234567890abcdef0(web-server) /dev/xvda1`
- Unattached volumes are named: `unattached`

### ENIs (Elastic Network Interfaces)
- If an ENI has no `Name` tag, it will be named after its attached resource
- **EC2 Instance ENIs**: Named after the instance (e.g., `web-server-eni`)
- **Service ENIs**: Named after the service type and resource name/ID
  - RDS: `rds-12345678-eni`
  - ElastiCache: `elasticache-12345678-eni`
  - ELB: `canvas-lb-sbx-eni` (extracted from load balancer name)
  - NAT Gateway: `nat-12345678-eni`
  - Lambda: `lambda-12345678-eni`
- The display shows the attached resource in format: `i-1234567890abcdef0 (web-server)` or `elb-canvas-lb-sbx`
- Unattached ENIs are named: `unattached-eni`

## Action History

The tool automatically tracks all tagging actions in `~/.quick-tag.yml` for auditing and review. Each action includes:

- **Account**: AWS account ID where the action was performed
- **Resource**: The AWS resource ID that was tagged
- **OldValue**: The previous name (empty if no previous name)
- **NewValue**: The new name that was applied
- **Timestamp**: Full ISO timestamp when the action was performed (RFC3339 format)
- **RunID**: Unique identifier for the execution run (useful for grouping related actions)
- **Undone**: Boolean flag indicating if this action has been undone (always present, defaults to false)

### Example History File

```yaml
actions:
  - Account: "123456789012"
    Resource: "i-1234567890abcdef0"
    OldValue: "instance-ami-12345678"
    NewValue: "amzn2-ami-hvm-2.0.20230920.0-x86_64-gp2"
    Timestamp: "2025-01-27T14:30:45Z"
    RunID: "run-a1b2c3d4e5f6"
    Undone: false
  - Account: "123456789012"
    Resource: "eni-08d422bbca1b21821"
    OldValue: ""
    NewValue: "web-server-eni"
    Timestamp: "2025-01-27T14:30:46Z"
    RunID: "run-a1b2c3d4e5f6"
    Undone: false
```

## Undo Functionality

The tool includes an undo feature that allows you to revert the last tagging run. This is useful for quickly undoing mistakes or changes that need to be rolled back.

### Usage

```bash
./quick_tag --undo
```

### How It Works

1. **Finds Last Run**: Identifies the most recent tagging run that hasn't been undone
2. **Shows Preview**: Displays all actions that will be reverted
3. **Confirms Action**: Asks for user confirmation before proceeding
4. **Reverts Tags**: Changes all resources back to their previous values
5. **Marks as Undone**: Updates the history to prevent double-undo

### Example Undo Session

```bash
$ ./quick_tag --undo
🔄 Undoing run run-a1b2c3d4e5f6 (3 actions):
  i-1234567890abcdef0: 'web-server' -> 'instance-ami-12345678'
  vol-1234567890abcdef0: 'i-1234567890abcdef0(web-server) /dev/xvda1' -> 'unattached-/dev/xvda1'
  eni-08d422bbca1b21821: 'web-server-eni' -> ''
Are you sure you want to undo these changes? (y/N): y
Reverting i-1234567890abcdef0: 'web-server' -> 'instance-ami-12345678'...
Reverting vol-1234567890abcdef0: 'i-1234567890abcdef0(web-server) /dev/xvda1' -> 'unattached-/dev/xvda1'...
Reverting eni-08d422bbca1b21821: 'web-server-eni' -> ''...
✅ Successfully undone 3/3 actions from run run-a1b2c3d4e5f6
```

### Safety Features

- **Confirmation Required**: Always asks for confirmation before undoing
- **No Double-Undo**: Prevents undoing the same run multiple times
- **Smart Error Handling**: Distinguishes between deleted resources and actual errors
- **Detailed Feedback**: Provides clear summary of what was reverted, skipped, or failed
- **History Tracking**: Maintains complete audit trail of undo operations

### Handling Deleted Resources

When undoing actions, the tool intelligently handles resources that no longer exist:

```bash
$ ./quick_tag --undo
🔄 Undoing run run-304d8c8a7259e585 (1 actions):
  eni-04f6490ff7a4e45f7: 'service-eni-attach-0c662a8fa2d049ab4-eni' -> ''
Are you sure you want to undo these changes? (y/N): y
Reverting eni-04f6490ff7a4e45f7: 'service-eni-attach-0c662a8fa2d049ab4-eni' -> ''...
Info: Resource eni-04f6490ff7a4e45f7 no longer exists (likely deleted) - skipping
✅ Undo completed for run run-304d8c8a7259e585:
   - Successfully reverted: 0 actions
   - Resources no longer exist: 1 actions (skipped)
📝 Marked all 1 actions from run run-304d8c8a7259e585 as undone in history
```

This prevents confusion when resources have been deleted since the original tagging operation.

## Requirements

- AWS CLI configured with appropriate credentials
- Go 1.24.4+ (for building from source)
- AWS permissions to:
  - `ec2:DescribeInstances`
  - `ec2:DescribeVolumes`
  - `ec2:DescribeNetworkInterfaces`
  - `ec2:DescribeImages`
  - `ec2:CreateTags`

## AWS Permissions

The following IAM permissions are required:

```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": [
                "ec2:DescribeInstances",
                "ec2:DescribeVolumes",
                "ec2:DescribeNetworkInterfaces",
                "ec2:DescribeImages",
                "ec2:CreateTags"
            ],
            "Resource": "*"
        }
    ]
}
```

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests if applicable
5. Submit a pull request

## License

This project is licensed under the MIT License - see the LICENSE file for details.

## Related Projects

- [quick-ecs](https://github.com/bevelwork/quick_ecs) - Quick ECS service management
- [quick-ssm](https://github.com/bevelwork/quick_ssm) - Quick SSM session management
