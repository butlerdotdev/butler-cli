# butler-cli

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

Command-line tools for the Butler Kubernetes-as-a-Service platform.

## Table of Contents

- [Overview](#overview)
- [Supported Infrastructure](#supported-infrastructure)
- [Installation](#installation)
- [butleradm](#butleradm)
- [butlerctl](#butlerctl)
- [Configuration](#configuration)
- [Architecture](#architecture)
- [Development](#development)
- [Contributing](#contributing)
- [License](#license)

## Overview

Butler provides two CLI tools designed for different audiences:

| Tool | Audience | Purpose |
|------|----------|---------|
| `butleradm` | Platform Operators | Manage the Butler platform itself |
| `butlerctl` | Platform Users | Consume the platform to run workloads |

This separation follows the same pattern as `kubeadm` and `kubectl` in the Kubernetes ecosystem. Platform operators bootstrap and maintain the infrastructure. Platform users create clusters and deploy applications.

Both tools interact with the same underlying system through Kubernetes Custom Resources. Whether you use the CLI, the web console, or `kubectl apply`, you are creating the same CRDs. Controllers handle the actual work.

## Supported Infrastructure

Butler supports management cluster deployment across on-premises hyperconverged infrastructure and public cloud platforms.

### On-Premises

| Provider | Status |
|----------|--------|
| Harvester HCI | Supported |
| Nutanix AHV | Planned |
| Proxmox VE | Planned |

### Public Cloud

| Provider | Status |
|----------|--------|
| AWS | Planned |
| Azure | Planned |
| Google Cloud | Planned |

Provider support is implemented through separate butler-provider-* controllers. The CLI and bootstrap architecture are provider-agnostic.

## Installation

### From Source

```sh
git clone https://github.com/butlerdotdev/butler-cli.git
cd butler-cli
make build
```

Binaries are placed in `./bin/`.

### Install to PATH

```sh
# Install to ~/bin
make install-local

# Install to /usr/local/bin (requires sudo)
sudo make install
```

### From Release

```sh
curl -LO https://github.com/butlerdotdev/butler-cli/releases/latest/download/butler-cli-linux-amd64.tar.gz
tar xzf butler-cli-linux-amd64.tar.gz
sudo mv butleradm butlerctl /usr/local/bin/
```

## butleradm

Platform administration tool for operators.

### Bootstrap a Management Cluster

The bootstrap command creates a production-ready Kubernetes management cluster on your infrastructure.

```sh
butleradm bootstrap harvester --config bootstrap.yaml
```

What happens:

1. A temporary KIND cluster is created on your local machine
2. Butler controllers are deployed to KIND
3. VMs are provisioned on your infrastructure
4. Talos Linux is configured on each VM
5. Kubernetes is bootstrapped
6. Platform addons are installed (Cilium, Longhorn, MetalLB, Steward, CAPI)
7. Kubeconfig is saved locally
8. KIND cluster is deleted

The resulting management cluster is self-sufficient and ready for tenant cluster provisioning.

### Example Configuration

```yaml
provider: harvester

cluster:
  name: butler-mgmt
  controlPlane:
    replicas: 3
    cpu: 4
    memoryMB: 16384
    diskGB: 100
  workers:
    replicas: 3
    cpu: 8
    memoryMB: 32768
    diskGB: 100

network:
  podCIDR: 10.244.0.0/16
  serviceCIDR: 10.96.0.0/12
  vip: 10.40.0.201

talos:
  version: v1.9.0
  schematic: dc7b152cb3ea99b821fcb7340ce7168313ce393d663740b791c36f6e95fc8586

addons:
  cni:
    type: cilium
  storage:
    type: longhorn
  loadBalancer:
    type: metallb
    addressPool: 10.40.0.200-10.40.0.250

providerConfig:
  harvester:
    kubeconfigPath: ~/.butler/harvester-kubeconfig
    namespace: default
    networkName: default/vlan40-workloads
    imageName: default/image-5rs6d
```

See `configs/examples/` for complete examples.

### Other Commands

```sh
butleradm status              # Platform health and status
butleradm upgrade             # Upgrade Butler components
butleradm backup              # Backup management cluster state
butleradm restore             # Restore from backup
```

## butlerctl

Platform user tool for developers and application teams.

### Cluster Operations

```sh
butlerctl cluster create my-app --workers 3    # Create tenant cluster
butlerctl cluster list                          # List all clusters
butlerctl cluster get my-app                    # Get cluster details
butlerctl cluster kubeconfig my-app             # Download kubeconfig
butlerctl cluster delete my-app                 # Delete cluster
```

### Addon Operations

```sh
butlerctl addon list                            # List available addons
butlerctl addon enable prometheus -c my-app     # Enable addon on cluster
butlerctl addon disable prometheus -c my-app    # Disable addon
```

### Access Operations

```sh
butlerctl access grant -c my-app -u alice@example.com --role admin
butlerctl access list -c my-app
butlerctl access revoke -c my-app -u alice@example.com
```

## Configuration

### Environment Variables

| Variable | Description |
|----------|-------------|
| `KUBECONFIG` | Path to management cluster kubeconfig |
| `BUTLER_CONFIG` | Path to CLI config file |

### Config File Locations

The CLI looks for configuration in the following order:

1. Path specified with `--config` flag
2. `./bootstrap.yaml` (current directory)
3. `~/.butler/config.yaml`

### Output Directory

Bootstrap outputs are saved to `~/.butler/`:

```
~/.butler/
├── <cluster>-kubeconfig      # Kubernetes kubeconfig
├── <cluster>-talosconfig     # Talos configuration
└── harvester-kubeconfig      # Provider credentials (user-provided)
```

## Architecture

Butler follows a Kubernetes-native, controller-based architecture. The CLIs are thin clients that create Custom Resources. Controllers running in the cluster perform the actual work.

This design provides:

- **Consistency**: Same CRs whether from CLI, Console, or kubectl
- **Resumability**: Controllers reconcile to desired state after interruption
- **Auditability**: All state in Kubernetes with standard RBAC
- **Extensibility**: Add new controllers without changing CLIs

For detailed architecture documentation, see [docs/architecture/DESIGN.md](docs/architecture/DESIGN.md).

## Development

### Prerequisites

- Go 1.24+
- Docker (for KIND during bootstrap testing)
- Access to infrastructure for integration testing

### Building

```sh
make build          # Build both CLIs
make butleradm      # Build butleradm only
make butlerctl      # Build butlerctl only
```

### Testing

```sh
make test           # Run unit tests
make lint           # Run linter
make fmt            # Format code
```

### Cross-Platform Builds

```sh
make dist           # Build for all platforms
make dist-linux     # Linux amd64 and arm64
make dist-darwin    # macOS amd64 and arm64
make dist-windows   # Windows amd64
```

### Project Structure

```
butler-cli/
├── cmd/
│   ├── butleradm/main.go
│   └── butlerctl/main.go
├── internal/
│   ├── adm/                    # butleradm implementation
│   │   ├── cmd/root.go
│   │   └── bootstrap/
│   │       ├── bootstrap.go
│   │       ├── harvester.go
│   │       ├── manifests/      # Embedded CRDs and controllers
│   │       └── orchestrator/
│   ├── ctl/                    # butlerctl implementation
│   │   ├── cmd/root.go
│   │   └── cluster/
│   └── common/                 # Shared packages
│       ├── client/
│       └── log/
├── configs/examples/
├── docs/
└── Makefile
```

## Related Projects

| Repository | Purpose |
|------------|---------|
| [butler-api](https://github.com/butlerdotdev/butler-api) | Shared CRD type definitions |
| [butler-bootstrap](https://github.com/butlerdotdev/butler-bootstrap) | Management cluster bootstrap controller |
| [butler-controller](https://github.com/butlerdotdev/butler-controller) | Tenant cluster lifecycle controller |
| [butler-provider-harvester](https://github.com/butlerdotdev/butler-provider-harvester) | Harvester VM provisioning controller |

## Contributing

Contributions are welcome. Please read the [contributing guidelines](CONTRIBUTING.md) before submitting a pull request.

## License

Copyright 2026 The Butler Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
