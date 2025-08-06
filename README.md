# Multipath Operator

A Kubernetes operator for managing device-mapper multipath configuration on OpenShift

## Overview

The Multipath Operator provides a cloud-native way to manage multipath storage
devices across your Kubernetes cluster. It automatically configures and manages
the multipath daemon on worker nodes, ensuring consistent multipath
configuration and high availability of storage devices.

## Architecture

The operator consists of:

1. **Custom Resource Definition (CRD)**: Defines the `Multipath` resource schema
1. **Controller**: Reconciles `Multipath` resources and manages daemon deployments
1. **DaemonSet**: Runs multipath daemon containers on selected nodes
1. **ConfigMap**: Stores generated multipath.conf configuration

## Quick Start

### Prerequisites

- OpenShift 4.19+
- Cluster admin privileges
- Nodes with multipath-capable storage devices

### Installation

1. **Clone the repository:**

   ```bash
   git clone https://github.com/mbaldessari/multipath-operator.git
   cd multipath-operator
   ```

1. **Install the CRD and operator:**

   ```bash
   ./scripts/multipath-operator-build.sh
   ```

### Basic Usage

1. **Create a Multipath resource:**

   ```bash
   kubectl apply -f config/samples/multipath_v1_multipath.yaml
   ```

1. **Check the status:**

   ```bash
   kubectl get multipath
   ```

1. **View detailed status:**

   ```bash
   kubectl describe multipath multipath-sample
   ```

## Configuration

### Multipath Resource Specification

```yaml
apiVersion: multipath.io/v1
kind: Multipath
metadata:
  name: example-multipath
spec:
  # Select nodes where multipath should run
  nodeSelector:
    node-role.kubernetes.io/worker: ""
  
  # Multipath daemon configuration
  config:
    defaults:
      user_friendly_names: "yes"
      find_multipaths: "yes"
      path_grouping_policy: "failover"
      # ... other defaults
    
    # Device-specific configurations
    devices:
    - vendor: "NetApp"
      product: "LUN.*"
      path_grouping_policy: "group_by_prio"
      # ... other device settings
    
    # Blacklist unwanted devices
    blacklist:
    - devnode: "^(ram|raw|loop|fd|md|dm-|sr|scd|st)[0-9]"
    
  # Update strategy for rolling updates
  updateStrategy:
    type: "RollingUpdate"
    rollingUpdate:
      maxUnavailable: 1
  
  # Security context (especially important for OpenShift)
  securityContext:
    privileged: true
    seLinuxOptions:
      type: "spc_t"
```

### Configuration Sections

#### Defaults

Global defaults applied to all multipath devices:

- `user_friendly_names`: Use friendly names for multipath devices
- `find_multipaths`: Automatically detect multipath devices
- `path_grouping_policy`: How to group paths (failover, multibus, group_by_prio)
- `path_checker`: Method to check path health (tur, rdac, hp_sw)

#### Devices

Device-specific overrides for particular storage arrays:

- `vendor`/`product`: Identify specific device types
- Storage-specific optimizations and features

#### Blacklist/Whitelist

Control which devices are managed by multipath:

- `blacklist`: Exclude devices from multipath management
- `blacklistExceptions`: Include devices despite blacklist rules

## OpenShift Considerations

### Node Selection

Use nodeSelector to target specific node types:

```yaml
nodeSelector:
  node-role.kubernetes.io/worker: ""
  # or target specific storage nodes
  storage-node: "true"
```

## Monitoring and Troubleshooting

### Check Operator Status

```bash
kubectl get pods -n multipath-system
kubectl logs -n multipath-system deployment/multipath-operator-controller-manager
```

### Check Multipath Status

```bash
kubectl get multipath -o wide
kubectl describe multipath <name>
```

### Check DaemonSet Status

```bash
kubectl get daemonset -n multipath-system
kubectl get pods -n multipath-system -l app=multipath-daemon
```

### Debug Multipath Configuration

```bash
# Check generated config
kubectl get configmap -n multipath-system multipath-config -o yaml

# Check multipath status on a node
kubectl exec -it <multipath-daemon-pod> -- multipath -ll
kubectl exec -it <multipath-daemon-pod> -- systemctl status multipathd
```

## Development

### Building

```bash
make build
```

### Testing

```bash
make test
```

### Building Container Image

```bash
make docker-build IMG=myregistry/multipath-operator:latest
make docker-push IMG=myregistry/multipath-operator:latest
```

### Generating Manifests

```bash
make manifests
```

## License

This project is licensed under the Apache License 2.0 - see the LICENSE file
for details.

## Support

- File issues at: <https://github.com/mbaldessari/multipath-operator/issues>
