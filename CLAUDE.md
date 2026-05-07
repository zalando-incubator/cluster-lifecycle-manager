# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

The Cluster Lifecycle Manager (CLM) is a Go-based tool for operating Kubernetes clusters on AWS. It can run as a CLI tool for direct cluster operations or as a controller continuously managing multiple clusters. CLM uses a cluster registry as the source of truth and applies versioned configurations from git repositories or local directories.

**Key Characteristics:**
- Designed to be reentrant - can be stopped and restarted at any point without losing state
- Supports two providers: `zalando-aws` (self-managed Kubernetes) and `zalando-eks` (AWS EKS)
- All cluster state is stored in the cluster registry and git configuration
- Uses Go templates for cluster configuration with support for config defaults and overrides

## Build and Test Commands

### Building
```bash
# Build for local platform (outputs to build/clm)
make
# or
make build.local

# Build for specific platforms
make build.linux     # Linux AMD64
make build.osx       # macOS AMD64

# Build Docker image
make build.docker    # Uses IMAGE and TAG env vars
```

### Testing
```bash
# Run all tests with race detection and coverage
make test

# Run tests for a specific package
go test -v ./controller/
go test -v ./provisioner/

# Run linter (uses golangci-lint)
make lint

# Format code
make fmt
```

### Running CLM Locally
```bash
# Provision/update a cluster
./build/clm provision \
  --registry=clusters.yaml \
  --token=$TOKEN \
  --directory=/path/to/configuration-folder \
  --debug

# Decommission a cluster
./build/clm decommission --registry=clusters.yaml --token=$TOKEN

# Render manifests without applying (useful for debugging or ArgoCD integration)
./build/clm render \
  --cluster-alias=my-cluster-alias \
  --registry=clusters.yaml \
  --directory=/path/to/configuration-folder

# Render manifests to a directory
./build/clm render \
  --cluster-alias=my-cluster-alias \
  --registry=clusters.yaml \
  --directory=/path/to/configuration-folder \
  --output-dir=./rendered-manifests

# Run as controller (continuous loop)
./build/clm controller --registry=https://cluster-registry.example.com --config-source=source:git:https://github.com/example/config.git
```

## Architecture

### Main Components

**Entry Point (`cmd/clm/main.go`)**
- Defines four commands: `provision`, `decommission`, `controller`, `render`
- Sets up AWS SDK configuration with retry logic
- Initializes provisioners, config sources, and cluster registry clients
- For `provision`/`decommission`: operates on clusters from registry then exits
- For `controller`: runs continuous loop with worker goroutines
- For `render`: renders manifests for a single cluster without applying (useful for ArgoCD config management plugins)

**Controller (`controller/`)**
- Implements the main control loop for continuously managing clusters
- Periodically refreshes cluster list from registry and config from sources
- Uses worker goroutines (configurable via `--concurrent-updates`) to process clusters in parallel
- Tracks cluster lifecycle status: `requested` → `ready` → `decommission-requested` → `decommissioned`
- Reports errors to cluster registry as Problems

**Provisioner (`provisioner/`)**
- Interface defining `Provision()`, `Decommission()`, and `Supports()` methods
- Two implementations: `ZalandoAWSProvisioner` and `ZalandoEKSProvisioner`
- `clusterpy.go` contains the core provisioning logic:
  - Applies CloudFormation stacks for infrastructure
  - Generates and applies Kubernetes manifests from templates
  - Manages node pools via Auto Scaling Groups
  - Handles declarative deletions (pre_apply and post_apply)
- Templates are rendered with cluster config items and channel configuration

**API (`api/`)**
- Core data structures: `Cluster`, `NodePool`, `ClusterVersion`, `ClusterStatus`
- `Cluster` contains: ID, alias, region, provider, node pools, config items, lifecycle status
- Cluster versioning based on SHA1 hash of cluster state + channel version

**Registry (`registry/`)**
- Interface for cluster registry operations
- Implementations for HTTP REST API and YAML file
- Used to list clusters, update lifecycle status, report problems

**Channel (`channel/`)**
- Abstraction for configuration sources: `Git`, `Directory`, or `CombinedSource`
- Supports multiple config sources that merge in order (later sources override earlier ones)
- Each channel maps to a versioned configuration (commit SHA for git, directory mtime for local)
- Configuration includes manifests, deletions.yaml, and config-defaults.yaml

**Update Strategy (`pkg/updatestrategy/`)**
- Implements non-disruptive rolling updates of cluster nodes
- Key files: `rolling_update.go`, `drain.go`, `node_pool_manager.go`
- Handles node draining with special care for stateful applications
- Manages Auto Scaling Group updates without disrupting workloads

**Kubernetes Client (`pkg/kubernetes/`)**
- Wrapper around client-go for applying manifests, draining nodes, managing resources
- Handles kubectl operations and cluster API interactions

**AWS Utilities (`pkg/aws/`)**
- AWS-specific operations: instance info lookup, EKS utilities
- Provides interfaces for mocking AWS services in tests

### Data Flow

1. **Controller Mode**: Registry → Controller → Config Source → Provisioner → AWS/K8s → Update Registry
2. **CLI Mode**: Registry → Config Source → Provisioner → AWS/K8s
3. Configuration is pulled from git/directory, merged with cluster registry data, templated, and applied

### Configuration System

**config-defaults.yaml**: Template file evaluated with cluster context to provide default config items (won't override existing ones)

**deletions.yaml**: Declarative deletion of deprecated resources
- `pre_apply`: deleted before applying manifests
- `post_apply`: deleted after applying manifests
- Resources identified by `name`, `labels`, or `selector`
- Supports deletion options: `propagation_policy`, `grace_period_seconds`, `has_owner`

**Multiple Config Sources**: When using multiple `--config-source` flags, sources are merged left-to-right with later sources taking precedence.

## Testing Patterns

- Unit tests use standard Go testing with table-driven tests
- AWS and Kubernetes clients are mocked via interfaces (`pkg/aws/iface/`, etc.)
- Test files follow Go conventions: `*_test.go` in same package
- Use `t.Run()` for subtests to group related test cases

## Code Style and Conventions

- Follow standard Go formatting (enforced by `gofmt` and `golangci-lint`)
- Use structured logging with logrus (`log.WithField()`, `log.WithFields()`)
- Error handling: wrap errors with context using `fmt.Errorf()` or `pkg/errors`
- AWS SDK v2 is used throughout (migrated from v1)
- Commit messages should be signed: `git commit -s`

## Key Patterns to Follow

1. **Provisioner Operations**: Always check `provisioner.Supports(cluster)` before calling methods
2. **Reentrant Design**: Operations should be idempotent and resumable
3. **Config Source Updates**: Call `configSource.Update()` to sync latest configuration before provisioning
4. **Secret Decryption**: Config items can contain encrypted values (AWS KMS) - use `secretDecrypter.Decrypt()`
5. **Template Rendering**: Cluster configurations are Go templates with access to `.ConfigItems`, `.Environment`, etc.
6. **Node Pool Management**: Node pools are updated using rolling updates via ASG with careful node draining

## Important Notes

- The cluster registry is the source of truth for cluster existence and desired state
- Changes to cluster configuration in git automatically trigger updates in controller mode
- The system is designed to safely handle long-running operations and interruptions
- When modifying provisioning logic, ensure operations remain idempotent
- Update strategy is critical for stateful workloads - changes here need careful testing

## Render Command

The `render` command renders manifests for a cluster without applying them. This enables integration with other tools and supports debugging:

1. Outputs rendered manifests to stdout by default
2. Applies all template transformations, config defaults, and decrypts secrets
3. Fetches AWS metadata (VPC, subnets, certificates) required by templates (with fallbacks to config items)
4. Can optionally write to a directory with `--output-dir` for local inspection

**Key design principles for render:**
- Does not require credentials for the target AWS account (skips account verification)
- Uses cluster alias (human-readable) instead of cluster ID for identification
- Supports optional config item overrides for AWS lookups:
  - `load_balancer_certificate`: ACM certificate ID (skips ACM lookup if provided)
  - `vpc_ipv4_cidr`: VPC CIDR block (skips VPC lookup if provided)
  - `vpc_ipv6_cidrs`: Comma-separated IPv6 CIDR blocks (skips VPC lookup if provided)

Usage: `clm render --cluster-alias=my-cluster --registry=<registry> --config-source=...`
