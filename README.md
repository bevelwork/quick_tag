# Quick Tag

A simple Go CLI for quickly tagging AWS EC2 instances, EBS volumes, and ENIs that don't have Name tags. 
It helps you discover untagged resources, suggests appropriate names based on their context, 
and provides an interactive interface for batch tagging operations — with fast, 
readable output designed for day-to-day AWS resource management.

Part of the `Quick Tools` family of tools from [Bevel Work](https://bevel.work/quick-tools).

## ✨ All Features

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

## Demo (examples)

```bash
quick-tag # Default 
quick-tag --region us-west-2 # Override profile region

AWS_PROFILE=my-profile quick-tag
aws-vault exec my-profile -- quick-tag
granted --profile my-profile quick-tag
```

## Install

### Required Software
- Go 1.24.4 or later
- AWS CLI configured with appropriate credentials

### Build from Brew
```bash
brew tap bevelwork/tap
brew install quick-tag
quick-tag --version
```

### Install with Go
```bash
go install github.com/bevelwork/quick_tag@latest
quick-tag --version
```

## Notes on Select Actions

### Tagging Logic
The tool only considers resources for tagging if they either have no Name tag or are using a quick-tag created tag that is no longer valid due to state changes (e.g., an ENI was listed as unattached and is now attached).

### Interactive Selection
- Choose which resources to tag using a numbered interface
- Select individual resources by number or use 'all' for batch operations
- Color-coded display: untagged (yellow), current tags (red), suggested tags (green)

### Undo Functionality
- Revert the last tagging run with `--undo` flag
- Shows preview of all actions that will be reverted
- Requires confirmation before proceeding
- Handles deleted resources gracefully

## Troubleshooting

- Authentication
  - If startup fails with authentication errors, confirm credentials and region.
  - `aws sts get-caller-identity` should work with your environment.

- Permissions
  - Your credentials need capabilities to call EC2 APIs used by the tool.
  - Required permissions: `ec2:DescribeInstances`, `ec2:DescribeVolumes`, `ec2:DescribeNetworkInterfaces`, `ec2:DescribeImages`, `ec2:CreateTags`

- Tagging Issues
  - The tool only tags resources that have no Name tag or have invalid quick-tag created tags
  - Resources with legitimate user-created tags will not be considered for retagging
  - Use `--undo` to revert the last tagging run if needed

## Version

The binary supports `--version` and prints either an ldflags-injected build version or a fallback development version.

## License

Apache 2.0
