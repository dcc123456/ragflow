#!/usr/bin/env python3
"""
GKE Cluster Configuration Helper Script

This script configures GKE cluster access, enables Gateway API, creates
GCS service account with Workload Identity, creates imagePullSecret for
GCR authentication, and generates the necessary configuration for
RAGFlow deployment.

NOTE: This script must be run on a GCE Ubuntu VM with full Cloud API access.

Prerequisites:
1. Create a GCE Ubuntu VM with full Cloud API access:

   Option A: Via GCP Console
   - Go to GCP Console > Compute Engine > VM Instances
   - Create a new VM with:
     * Service Account: Select or create a service account with these roles:
       - Kubernetes Engine Admin (roles/container.admin)
       - Storage Object Admin (roles/storage.objectAdmin)
       - Service Usage Consumer (roles/servicemanagement.usageServiceConsumer)
   - Or use the default Compute Engine service account with these roles

   Option B: Via gcloud CLI:
   gcloud compute instances create YOUR_VM_NAME \\
     --project=YOUR_PROJECT_ID \\
     --zone=YOUR_ZONE \\
     --scopes=cloud-platform \\
     --machine-type=e2-highcpu-8 \\
     --boot-disk-size=200 \\
     --image-project=ubuntu-os-cloud \\
     --image-family=ubuntu-2404-lts-amd64

   This creates a VM with cloud-platform scope (full API access).

Usage:
    python3 gke_setup.py [-h]
"""

import os
import sys
import subprocess
import json
import shutil
import tempfile


def check_kubectl_installed():
    """Check if kubectl is installed, install if not.

    Reference: https://kubernetes.io/docs/tasks/tools/install-kubectl-linux/
    """
    # First check if kubectl is installed
    result = run_cmd("which kubectl", check=False)
    if result.returncode == 0:
        print("kubectl is already installed.")
        return

    print("kubectl is not installed. Installing via apt...")

    # Add Kubernetes apt repository
    run_cmd("sudo apt-get install -y apt-transport-https ca-certificates curl gnupg", check=False)
    run_cmd("sudo curl -fsSL https://pkgs.k8s.io/core:/stable:/v1.28/deb/Release.key | sudo gpg --batch --yes --dearmor -o /usr/share/keyrings/kubernetes-archive-keyring.gpg", check=False)
    run_cmd('echo "deb [signed-by=/usr/share/keyrings/kubernetes-archive-keyring.gpg] https://pkgs.k8s.io/core:/stable:/v1.28/deb/ /" | sudo tee /etc/apt/sources.list.d/kubernetes.list', check=False)

    run_cmd("sudo apt-get update -qq", check=False)
    result = run_cmd("sudo apt-get install -y kubectl", check=True)

    if result.returncode == 0:
        print("  kubectl installed successfully!")
    else:
        print("\nAutomatic installation failed.")
        print("Manual installation:")
        print('  curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl"')
        print("  sudo install -o root -g root -m 0755 kubectl /usr/local/bin/kubectl")
        sys.exit(1)


def print_help():
    """Print the help message with prerequisites."""
    print(__doc__)


GCR_PROJECT_PREFIX = "gcr.io"

# =============================================================================
# ALERTING APPROACHES: COMPARISON
# =============================================================================
#
# This module supports three approaches for alerting on Kubernetes pod issues:
#
# APPROACH 1: Log-Based Metrics
# ------------------------------------
# Pros:
#   + Easy to set up, no additional components needed
#   + Uses existing Cloud Logging infrastructure
#   + Can filter on any log field via log filter expressions
# Cons:
#   - EVENT-BASED, NOT STATE-BASED: Only triggers on new events, not persistent issues
#   - Example problem: Pod enters CrashLoopBackOff, event fires alert,
#     but if pod stays crashed without new events, alert auto-resolves
#     even though the pod is still down!
#   - No distinction between severity levels in a single metric
#   - Requires careful filter design to avoid noise
#
#   Filter example: log_id("events") AND resource.type="k8s_pod" AND jsonPayload.involvedObject.kind="Pod" AND jsonPayload.involvedObject.namespace="ragflow"
#
# APPROACH 2: GKE Workload Metrics (kubernetes.io/*)
# ------------------------------------
# Pros:
#   + Built into GKE, no extra installation
#   + Provides actual resource state, not just events
#   + Metrics like: kubernetes.io/container/state/restart_count
#   + Good for basic pod health monitoring
# Cons:
#   - Limited metric set compared to Prometheus
#   - No PromQL, only supports simple condition filters
#   - Cannot easily express complex queries (e.g., "restart count increased by 3 in 5 min")
#   - Less flexible for custom alerting logic
#
#   Metric example: kubernetes.io/container/state/restart_count
#   Condition: last_value() > 0 for 5m
#
# APPROACH 3: Google Managed Prometheus (GMP) + PromQL (RECOMMENDED)
# ------------------------------------
# Pros:
#   + STATE-BASED: Monitors actual pod state continuously, not just events
#   + Pod crash (CrashLoopBackOff) keeps alerting until pod actually recovers
#   + Full PromQL expressiveness for complex queries
#   + Industry standard, widely understood
#   + Rich kube-state-metrics available:
#     * kube_pod_status_phase{phase!="Running"}
#     * kube_pod_container_status_restarts_total
#     * kube_deployment_status_replicas_available
#   + Supports "for" clause to require condition persistence
#   + Can correlate multiple signals (pod down + high memory + many restarts)
# Cons:
#   - Requires GKE with managed Prometheus enabled (GKE 1.27+)
#   - Slightly more complex setup
#   - Need to enable GMP on the cluster
#
#   PromQL example:
#     kube_pod_status_phase{namespace="ragflow",phase!="Running"} == 1
#     [for: 2m]  # Condition must persist for 2 minutes
#
# GCP Alert Policies support PromQL through conditionPrometheusQueryLanguage.
# This allows us to use increase(), rate(), etc. for proper state-based alerting.
#
# To enable GMP on GKE:
#   gcloud container clusters update CLUSTER_NAME --region=REGION --enable-managed-prometheus
#
# =============================================================================


def run_cmd(cmd, check=True, capture_output=True, timeout=60):
    """Run a shell command and return the result.

    Args:
        cmd: A shell command string to be executed with shell=True
        check: If True, exit on command failure; if False, silently handle errors
        capture_output: If True, capture stdout and stderr
        timeout: Timeout in seconds (default 60)
    """
    # Always use shell=True for string commands
    result = subprocess.run(cmd, capture_output=capture_output, text=True, check=False, shell=True, timeout=timeout)
    if check and result.returncode != 0:
        print(f"Error running command: {cmd}")
        print(f"stderr: {result.stderr}")
        sys.exit(1)
    # Only print error info when check=True (caller expects success)
    return result


def check_gcloud_installed():
    """Check if gcloud is installed, install if not.

    Reference: https://cloud.google.com/sdk/docs/install#deb
    """
    # First check if gcloud is installed via snap and remove it
    # Note: snap gcloud does not support gke-gcloud-auth-plugin, which is required
    # for GKE cluster authentication. Must use apt or standalone installation instead.
    result = run_cmd("ls /snap/bin/gcloud", check=False)
    if result.returncode == 0:
        print("Found gcloud installed via snap. Removing it first (snap does not support gke-gcloud-auth-plugin)...")
        # Use --purge, --non-interactive flags
        run_cmd("sudo snap remove google-cloud-cli --purge", check=False)
        # Also remove the symlinks and directory
        run_cmd("sudo rm -f /snap/bin/gcloud", check=False)
        run_cmd("sudo rm -rf /snap/google-cloud-cli", check=False)
        # Verify removal
        result = run_cmd("ls /snap/bin/gcloud", check=False)
        if result.returncode == 0:
            print("Warning: gcloud still found after snap removal, trying manual removal...")
            run_cmd("sudo rm -f /snap/bin/gcloud", check=False)
            run_cmd("sudo rm -rf /snap/google-cloud-cli", check=False)

    # Check if gcloud is now available (should NOT be after removing snap)
    result = run_cmd("which gcloud", check=False)
    if result.returncode != 0:
        print("gcloud is not installed. Installing via apt...")

        # Add Google Cloud SDK repository with proper GPG key
        run_cmd(
            'echo "deb [signed-by=/usr/share/keyrings/cloud.google.gpg] https://packages.cloud.google.com/apt cloud-sdk main" | sudo tee /etc/apt/sources.list.d/google-cloud-sdk.list', check=False
        )

        # Download GPG key using wget with the correct URL (fallback to different method)
        gpg_result = run_cmd(
            "curl -fsSL -o /tmp/google-cloud.gpg https://packages.cloud.google.com/apt/doc/apt-key.gpg && sudo gpg --batch --yes --dearmor -o /usr/share/keyrings/cloud.google.gpg /tmp/google-cloud.gpg",
            check=False,
        )
        if gpg_result.returncode != 0:
            # Fallback: try using wget and different key URL
            print("  Primary GPG key download failed, trying alternative method...")
            run_cmd("sudo wget -qO /usr/share/keyrings/cloud.google.gpg https://dl.google.com/linux/linux_signing_key.pub", check=False)

        run_cmd("sudo apt-get update -qq", check=False)
        result = run_cmd("sudo apt-get install -y google-cloud-cli", check=False)

        if result.returncode == 0:
            print("  gcloud installed successfully!")
            return

        # Final fallback: use standalone installer
        print("  apt install failed, trying standalone installer...")
        run_cmd("curl -fsSL https://sdk.cloud.google.com | bash", check=False)
        # Source the profile to make gcloud available
        result = run_cmd("/bin/bash -c 'source /etc/profile && which gcloud'", check=False)
        if result.returncode == 0:
            print("  gcloud installed via standalone installer!")
            return

        print("\nAutomatic installation failed.")
        print("Manual installation:")
        print("  curl -fsSL https://sdk.cloud.google.com | bash")
        sys.exit(1)


def check_gke_auth_plugin():
    """Check and install gke-gcloud-auth-plugin."""
    print("Checking gke-gcloud-auth-plugin...")

    # Check if already installed
    result = run_cmd("which gke-gcloud-auth-plugin", check=False)
    if result.returncode == 0:
        print("  gke-gcloud-auth-plugin is already installed.")
        return

    print("  gke-gcloud-auth-plugin not found. Installing via apt...")

    # Check if apt repository is configured
    repo_check = run_cmd("ls /etc/apt/sources.list.d/google-cloud-sdk.list", check=False)
    if repo_check.returncode != 0:
        print("  Adding Google Cloud SDK apt repository...")
        # Add the repository
        run_cmd(
            'echo "deb [signed-by=/usr/share/keyrings/cloud.google.gpg] https://packages.cloud.google.com/apt cloud-sdk main" | sudo tee /etc/apt/sources.list.d/google-cloud-sdk.list', check=False
        )
        # Add the key
        run_cmd(
            "curl -fsSL -o /tmp/google-cloud.gpg https://packages.cloud.google.com/apt/doc/apt-key.gpg && sudo gpg --batch --yes --dearmor -o /usr/share/keyrings/cloud.google.gpg /tmp/google-cloud.gpg",
            check=False,
        )
        run_cmd("sudo apt-get update -qq", check=False)

    # Install via apt
    result = run_cmd("sudo apt-get install -y google-cloud-cli-gke-gcloud-auth-plugin", check=False)
    if result.returncode == 0:
        print("  Installed successfully via apt.")
        # Verify the plugin is now available
        result = run_cmd("which gke-gcloud-auth-plugin", check=False)
        if result.returncode == 0:
            print("  gke-gcloud-auth-plugin is now available.")
            return
        print("  Warning: Plugin installed but not found in PATH, trying gcloud components...")

    print("  Trying gcloud components install...")
    # Try gcloud components as fallback
    result = run_cmd("gcloud components install gke-gcloud-auth-plugin --quiet", check=False)
    if result.returncode == 0:
        print("  Installed via gcloud components.")
        return

    print("  Warning: Could not install gke-gcloud-auth-plugin.")
    print("  Will try gcloud container clusters get-credentials which auto-installs the plugin.")
    print("  You can also manually install via apt: sudo apt-get install -y google-cloud-cli-gke-gcloud-auth-plugin")


def configure_docker_for_gcr():
    """Configure docker to authenticate with GCR (Google Container Registry).

    Uses --quiet flag to run non-interactively.
    """
    print("\nConfiguring docker for GCR authentication...")

    # Check if docker is installed
    result = run_cmd("which docker", check=False)
    if result.returncode != 0:
        print("  Warning: docker is not installed. Skipping GCR configuration.")
        return

    # Configure docker for GCR non-interactively
    result = run_cmd("gcloud auth configure-docker --quiet", check=False)

    if result.returncode == 0:
        print("  Docker configured for GCR successfully!")
    else:
        print(f"  Warning: Could not configure docker: {result.stderr}")


def list_gke_clusters():
    """List available GKE clusters and let user select one."""
    print("\n" + "=" * 70)
    print("AVAILABLE GKE CLUSTERS:")
    print("=" * 70)

    # Get current project
    result = run_cmd("gcloud config get-value project", check=False)
    project = result.stdout.strip()
    if not project:
        print("Error: No project configured. Run 'gcloud config set project YOUR_PROJECT_ID'")
        sys.exit(1)

    print(f"Project: {project}\n")

    # List clusters
    result = run_cmd(f"gcloud container clusters list --project={project} --format=json", check=False)
    if result.returncode != 0:
        print(f"Error listing clusters: {result.stderr}")
        sys.exit(1)

    try:
        clusters = json.loads(result.stdout)
    except json.JSONDecodeError:
        clusters = []

    if not clusters:
        print("No GKE clusters found in this project.")
        print("Create a cluster first: gcloud container clusters create CLUSTER_NAME --region REGION")
        sys.exit(1)

    # Display clusters
    print(f"{'#':<4} {'Name':<30} {'Region':<15} {'Status':<10}")
    print("-" * 70)
    for i, cluster in enumerate(clusters, 1):
        name = cluster.get("name", "N/A")
        location = cluster.get("location", "N/A")
        status = cluster.get("status", "N/A")
        print(f"{i:<4} {name:<30} {location:<15} {status:<10}")

    # Select cluster
    print()
    if len(clusters) == 1:
        selected = clusters[0]
        print(f"Auto-selecting the only cluster: {selected['name']}")
    else:
        while True:
            try:
                choice = input(f"Select cluster (1-{len(clusters)}): ").strip()
                idx = int(choice) - 1
                if 0 <= idx < len(clusters):
                    selected = clusters[idx]
                    break
            except ValueError:
                pass
            print(f"Invalid selection. Please enter a number between 1 and {len(clusters)}")

    return selected, project


def get_cluster_region(cluster_name, project):
    """Get the region/zone for a cluster."""
    # First get the location from the list
    result = run_cmd(f"gcloud container clusters list --project={project} --format=json", check=False)
    if result.returncode == 0:
        clusters = json.loads(result.stdout)
        for c in clusters:
            if c["name"] == cluster_name:
                location = c.get("location", "us-central1")
                # Determine if regional or zonal
                if "-" in location and location[-2:].isdigit():
                    return location.rsplit("-", 1)[0]
                return location
    return "us-central1"  # Default fallback


def get_cluster_info(cluster, project):
    """Get cluster endpoint and CA certificate."""
    name = cluster["name"]
    location = cluster["location"]

    # Determine if regional or zonal
    if "-" in location and location[-2:].isdigit():
        # Zonal cluster (e.g., us-central1-a)
        region = location.rsplit("-", 1)[0]
        zone = location
    else:
        # Regional cluster
        region = location
        zone = None

    cmd = f"gcloud container clusters describe {name} --project={project} --format=json"
    if zone:
        cmd += f" --zone={zone}"
    else:
        cmd += f" --region={region}"

    result = run_cmd(cmd)
    cluster_info = json.loads(result.stdout)

    endpoint = cluster_info.get("endpoint")
    master_auth = cluster_info.get("masterAuth", {})
    cluster_ca_cert = master_auth.get("clusterCaCertificate", "")

    return endpoint, cluster_ca_cert, region, zone


def enable_public_access(project, cluster_name, region, zone):
    """Enable public access to the cluster control plane."""
    print(f"\nEnabling public access to cluster {cluster_name}...")

    cmd = f"gcloud container clusters update {cluster_name} --project={project} --no-enable-master-authorized-networks"
    if zone:
        cmd += f" --zone={zone}"
    else:
        cmd += f" --region={region}"

    result = run_cmd(cmd, check=False)
    if result.returncode == 0:
        print("  Public access enabled.")
    else:
        print(f"  Warning: {result.stderr}")


def enable_gateway_api(project, cluster_name, region, zone):
    """Enable Gateway API on the GKE cluster."""
    print(f"\nEnabling Gateway API on cluster {cluster_name}...")

    # Use --gateway-api=standard to enable Gateway API
    cmd = f"gcloud container clusters update {cluster_name} --project={project} --gateway-api=standard"
    if zone:
        cmd += f" --zone={zone}"
    else:
        cmd += f" --region={region}"

    run_cmd(cmd, check=True)
    print("  Gateway API enabled.")


def enable_managed_prometheus(project, cluster_name, region, zone):
    """Enable Managed Prometheus on the GKE cluster."""
    print(f"\nEnabling Managed Prometheus on cluster {cluster_name}...")

    cmd = f"gcloud container clusters update {cluster_name} --project={project} --enable-managed-prometheus"
    if zone:
        cmd += f" --zone={zone}"
    else:
        cmd += f" --region={region}"

    run_cmd(cmd, check=True)
    print("  Managed Prometheus enabled.")


def create_proxy_only_subnet(project, cluster_name, region):
    """
    Create a proxy-only subnet for GKE Gateway in the cluster's VPC network.

    GKE Gateway requires a proxy-only subnet in the same VPC and region as the cluster.
    This subnet is used by the global external HTTP(S) load balancer.

    Args:
        project: GCP project ID
        cluster_name: GKE cluster name
        region: GCP region (e.g., asia-east2)
    """
    print("\n" + "=" * 70)
    print("Creating Proxy-Only Subnet for GKE Gateway")
    print("=" * 70)

    # Get cluster network configuration
    print("\nFetching cluster network configuration...")
    cmd = f"gcloud container clusters describe {cluster_name} --project={project} --region={region} --format=json"
    result = run_cmd(cmd)
    cluster_info = json.loads(result.stdout)

    network = cluster_info.get("network", "default")
    subnetwork = cluster_info.get("subnetwork", "")

    print(f"  Cluster: {cluster_name}")
    print(f"  Network: {network}")
    print(f"  Subnetwork: {subnetwork}")
    print(f"  Region: {region}")

    # Check if proxy-only subnet already exists
    subnet_name = f"proxy-only-subnet-{region}"
    print(f"\nChecking for existing proxy-only subnet '{subnet_name}'...")

    cmd = f"gcloud compute networks subnets describe {subnet_name} --region={region} --project={project} --format=json"
    result = run_cmd(cmd, check=False)

    if result.returncode == 0:
        print(f"  Proxy-only subnet '{subnet_name}' already exists in region {region}.")
        existing_subnet = json.loads(result.stdout)
        existing_range = existing_subnet.get("ipCidrRange", "")
        existing_purpose = existing_subnet.get("purpose", "")
        print(f"    Range: {existing_range}")
        print(f"    Purpose: {existing_purpose}")

        # Check if it's the right purpose
        if "MANAGED_PROXY" in existing_purpose:
            print("  ✓ Proxy-only subnet is properly configured.")
            return
        else:
            print(f"  Warning: Subnet exists but has wrong purpose: {existing_purpose}")
            print("  Deleting and recreating...")
            cmd = f"gcloud compute networks subnets delete {subnet_name} --region={region} --project={project} --quiet"
            run_cmd(cmd, check=False)
    else:
        print("  Proxy-only subnet does not exist. Creating new one...")

    # Select CIDR range for proxy-only subnet
    # Use a non-overlapping range. Common choices: 10.120.0.0/23, 10.129.0.0/23
    # Must not overlap with existing subnets in the VPC
    proxy_cidr = "10.120.0.0/23"  # Default, can be customized

    print("\nCreating proxy-only subnet...")
    print(f"  Name: {subnet_name}")
    print(f"  Network: {network}")
    print(f"  Region: {region}")
    print(f"  Range: {proxy_cidr}")
    print("  Purpose: REGIONAL_MANAGED_PROXY")
    print("  Role: ACTIVE")

    cmd = f"gcloud compute networks subnets create {subnet_name} "
    cmd += f"--project={project} "
    cmd += f"--network={network} "
    cmd += f"--region={region} "
    cmd += f"--range={proxy_cidr} "
    cmd += "--purpose=REGIONAL_MANAGED_PROXY "
    cmd += "--role=ACTIVE"

    result = run_cmd(cmd, check=False)

    if result.returncode != 0:
        error_output = result.stderr + result.stdout
        # Only retry with alternative CIDRs if it's a CIDR/IP range conflict error
        if "IP_RANGE" in error_output or "range is already" in error_output.lower() or "conflicts with" in error_output.lower():
            print("  First attempt failed (CIDR conflict). Trying alternative CIDR range...")
            # Try different CIDR ranges
            alternative_cidrs = ["10.129.0.0/23", "10.130.0.0/23", "10.131.0.0/23"]

            for alt_cidr in alternative_cidrs:
                print(f"  Trying CIDR: {alt_cidr}")
                cmd = f"gcloud compute networks subnets create {subnet_name} "
                cmd += f"--project={project} "
                cmd += f"--network={network} "
                cmd += f"--region={region} "
                cmd += f"--range={alt_cidr} "
                cmd += "--purpose=REGIONAL_MANAGED_PROXY "
                cmd += "--role=ACTIVE"

                result = run_cmd(cmd, check=False)
                if result.returncode == 0:
                    print(f"  ✓ Successfully created proxy-only subnet with range {alt_cidr}")
                    break
            else:
                print("  Error: Could not create proxy-only subnet with any of the attempted CIDR ranges.")
                print("  Please check the existing subnets in the VPC and specify a non-overlapping CIDR range.")
                print("\n  To list existing subnets:")
                print(f"    gcloud compute networks subnets list --project={project} --regions={region}")
                sys.exit(1)
        else:
            # Non-CIDR error (e.g., permission denied)
            print(f"  Error creating subnet: {error_output}")
            print("  Please check that the service account has 'Compute Network Admin' role.")
            sys.exit(1)

    print("\n  ✓ Proxy-only subnet created successfully!")
    print("\nNote: GKE Gateway will automatically use this proxy-only subnet for")
    print("      creating global external HTTP(S) load balancers.")


def create_kubeconfig(endpoint, cluster_ca_cert, cluster_name, project):
    """Generate kubeconfig file using gke-gcloud-auth-plugin."""
    print("\nGenerating kubeconfig...")

    # Ensure .kube directory exists
    kube_dir = os.path.expanduser("~/.kube")
    os.makedirs(kube_dir, exist_ok=True)

    # Check if gke-gcloud-auth-plugin is already available
    plugin_available = run_cmd("which gke-gcloud-auth-plugin", check=False).returncode == 0

    # Get token for backup kubeconfig
    token_result = run_cmd("gcloud auth print-access-token", check=False)
    token = token_result.stdout.strip()

    if plugin_available:
        # Plugin is available, use it
        print("  gke-gcloud-auth-plugin is available. Using exec-based kubeconfig...")
        region = get_cluster_region(cluster_name, project)
        cred_result = run_cmd(f"gcloud container clusters get-credentials {cluster_name} --region={region} --project={project}", check=False)
        if cred_result.returncode == 0:
            print("  Kubeconfig generated successfully with gke-gcloud-auth-plugin!")
            return
        print(f"  gcloud get-credentials failed: {cred_result.stderr}")

    # Plugin not available, try to install it first
    print("  gke-gcloud-auth-plugin not available. Attempting to install...")

    # Try apt install
    apt_result = run_cmd("sudo apt-get install -y google-cloud-cli-gke-gcloud-auth-plugin", check=False)
    if apt_result.returncode == 0:
        plugin_available = run_cmd("which gke-gcloud-auth-plugin", check=False).returncode == 0
        if plugin_available:
            print("  Plugin installed successfully via apt!")
            region = get_cluster_region(cluster_name, project)
            cred_result = run_cmd(f"gcloud container clusters get-credentials {cluster_name} --region={region} --project={project}", check=False)
            if cred_result.returncode == 0:
                print("  Kubeconfig generated successfully with gke-gcloud-auth-plugin!")
                return

    # Try gcloud components install
    print("  Trying gcloud components install...")
    comp_result = run_cmd("gcloud components install gke-gcloud-auth-plugin --quiet", check=False)
    if comp_result.returncode == 0:
        plugin_available = run_cmd("which gke-gcloud-auth-plugin", check=False).returncode == 0
        if plugin_available:
            print("  Plugin installed via gcloud components!")
            region = get_cluster_region(cluster_name, project)
            cred_result = run_cmd(f"gcloud container clusters get-credentials {cluster_name} --region={region} --project={project}", check=False)
            if cred_result.returncode == 0:
                print("  Kubeconfig generated successfully with gke-gcloud-auth-plugin!")
                return

    # All installation attempts failed, fall back to token-based auth
    print("  Failed to install gke-gcloud-auth-plugin.")
    print("  Falling back to token-based auth...")
    print("  Note: Token expires in ~1 hour. For permanent credentials:")
    print("    sudo apt-get install -y google-cloud-cli-gke-gcloud-auth-plugin")

    # Get token
    token_result = run_cmd("gcloud auth print-access-token", check=False)
    token = token_result.stdout.strip()

    # Write token-based kubeconfig
    kubeconfig = f"""apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: {cluster_ca_cert}
    server: https://{endpoint}
  name: gke_{project}_{cluster_name}
contexts:
- context:
    cluster: gke_{project}_{cluster_name}
    user: gke_{project}_{cluster_name}
  name: gke_{project}_{cluster_name}
current-context: gke_{project}_{cluster_name}
kind: Config
preferences: {{}}
users:
- name: gke_{project}_{cluster_name}
  user:
    token: {token}
"""
    config_path = os.path.join(kube_dir, "config")
    with open(config_path, "w") as f:
        f.write(kubeconfig)
    print(f"  Kubeconfig saved to {config_path}")

    # Generate token-based kubeconfig with timestamp suffix for other environments
    import datetime

    timestamp = datetime.datetime.now().strftime("%Y%m%d_%H%M%S")

    token_kubeconfig = f"""apiVersion: v1
clusters:
- cluster:
    certificate-authority-data: {cluster_ca_cert}
    server: https://{endpoint}
  name: gke_{project}_{cluster_name}
contexts:
- context:
    cluster: gke_{project}_{cluster_name}
    user: gke_{project}_{cluster_name}
  name: gke_{project}_{cluster_name}
current-context: gke_{project}_{cluster_name}
kind: Config
preferences: {{}}
users:
- name: gke_{project}_{cluster_name}
  user:
    token: {token}
"""

    token_config_path = os.path.join(kube_dir, f"config_{timestamp}")
    with open(token_config_path, "w") as f:
        f.write(token_kubeconfig)
    print(f"  Token-based kubeconfig (expires in ~1 hour) saved to {token_config_path}")

    # Test connection
    print("\nTesting kubectl connection...")
    result = run_cmd("kubectl cluster-info", check=False)
    if result.returncode == 0:
        print("  kubectl connection successful!")
    else:
        print(f"  Warning: kubectl test failed: {result.stderr}")


def update_terraform_tfvars(variables, example_file=None):
    """Update terraform.tfvars with multiple variables.

    Args:
        variables: Dictionary of variable names to values
        example_file: Optional path to example file to copy from if tfvars doesn't exist
    """
    print("\nUpdating terraform.tfvars...")

    tfvars_path = "terraform.tfvars"

    # If tfvars_path doesn't exist, copy from example file
    if not os.path.exists(tfvars_path) and example_file:
        if os.path.exists(example_file):
            print(f"  Copying {example_file} to {tfvars_path}")
            shutil.copy(example_file, tfvars_path)

    # Read existing file
    lines = []
    if os.path.exists(tfvars_path):
        with open(tfvars_path, "r") as f:
            lines = f.readlines()

    # Track which variables were updated or added
    updated_vars = set()
    new_lines = []

    for line in lines:
        stripped = line.strip()
        # Check if this line starts with any of our variable names
        matched = False
        for var_name in variables:
            if stripped.startswith(var_name):
                new_lines.append(f'{var_name} = "{variables[var_name]}"\n')
                updated_vars.add(var_name)
                matched = True
                break
        if not matched:
            new_lines.append(line)

    # Add any variables that weren't found in the file
    for var_name, value in variables.items():
        if var_name not in updated_vars:
            new_lines.append(f'\n{var_name} = "{value}"\n')
            updated_vars.add(var_name)

    with open(tfvars_path, "w") as f:
        f.writelines(new_lines)

    # Print updated values
    for var_name, value in variables.items():
        print(f'  Set {var_name} = "{value}"')


def create_gcs_bucket(project, cluster_id, region, gke_namespace="ragflow"):
    """Create GCS bucket for RAGFlow storage (if not exists) and configure IAM.

    Args:
        project: GCP project ID
        cluster_id: GKE cluster ID (used to generate bucket name)
        region: GCP region where the bucket should be created
        gke_namespace: Kubernetes namespace for RAGFlow (used for Workload Identity binding)

    Note on Region Selection:
        While cross-region deployment is technically feasible, it has two significant drawbacks:

        1. Network Egress Costs:
           - If GKE and GCS are in the same region, data transfer is typically free
             (uses Google's internal network).
           - If they are in different regions, Google charges Cross-region Data Transfer
             fees when reading large amounts of data from GCS to GKE. For TB-scale data,
             these costs can be substantial.

        2. Latency:
           - Same-region access typically offers single-digit millisecond latency.
           - Cross-region access can add tens to hundreds of milliseconds of latency,
             which may impact real-time applications (e.g., real-time image processing).

        Recommendation: Create the GCS bucket in the same region as your GKE cluster
        to avoid these extra costs and latency.

    The bucket is configured with:
    - Bucket-level IAM binding for the GCP service account used by RAGFlow
    - Workload Identity binding between K8s default SA and GCP SA
    """
    print("\n" + "=" * 70)
    print("Creating GCS Bucket for RAGFlow")
    print("=" * 70)

    # Fixed bucket name based on GKE cluster ID (shortened to 63 chars max)
    # GKE cluster ID is very long (64 chars), so we take the first 40 chars
    # ragflow- (8 chars) + 40 chars = 48 chars, well within 63 char limit
    short_cluster_id = cluster_id.lower()[:40]
    bucket_name = f"ragflow-{short_cluster_id}"

    # Check if bucket already exists
    print(f"\nChecking if bucket '{bucket_name}' exists...")
    result = run_cmd(f"gcloud storage buckets list --filter='name:{bucket_name}' --format='value(name)'", check=False)

    bucket_existed = result.returncode == 0
    if bucket_existed:
        print(f"  Bucket '{bucket_name}' already exists.")
    else:
        # Create bucket
        print(f"  Bucket not found. Creating '{bucket_name}'...")
        result = run_cmd(f"gcloud storage buckets create gs://{bucket_name}/ --project={project} --location={region}", check=False)

        if result.returncode == 0:
            print(f"  Bucket '{bucket_name}' created successfully!")
        else:
            print(f"  Error: Could not create bucket: {result.stderr}")
            sys.exit(1)

    # Get project number for service account emails
    result = run_cmd(f"gcloud projects describe {project} --format='value(projectNumber)'", check=False)
    if result.returncode != 0:
        print(f"  Warning: Could not get project number: {result.stderr}")
        return bucket_name

    project_number = result.stdout.strip()

    # GCP service account for RAGFlow (created separately or used as is)
    gcp_sa_email = f"ragflow-gcs@{project}.iam.gserviceaccount.com"

    # Node service account (used by GKE nodes via Workload Identity)
    node_sa_email = f"{project_number}-compute@developer.gserviceaccount.com"

    # Add bucket-level IAM bindings for both node SA and GCP SA
    print(f"\nConfiguring bucket IAM for '{bucket_name}'...")

    # Grant storage.admin to node SA and GCP SA (storage.admin is required for bucket.exists() check, storage.objectAdmin is not enough)
    for sa_email in [node_sa_email, gcp_sa_email]:
        result = run_cmd(
            f"gcloud storage buckets add-iam-policy-binding gs://{bucket_name} --member=serviceAccount:{sa_email} --role=roles/storage.admin",
            check=False,
        )
        if result.returncode == 0:
            print(f"  storage.admin granted to {sa_email}")
        elif "already exists" in result.stderr.lower() or "duplicate" in result.stderr.lower():
            print(f"  storage.admin already exists for {sa_email}")
        else:
            print(f"  Warning: Could not grant storage.admin to {sa_email}: {result.stderr}")

    # Set up Workload Identity binding for K8s default SA to GCP SA
    # This allows pods using the default K8s SA to impersonate the GCP SA
    print(f"\nSetting up Workload Identity binding for K8s namespace '{gke_namespace}'...")

    workload_pool = f"{project}.svc.id.goog"
    k8s_sa_member = f"serviceAccount:{workload_pool}[{gke_namespace}/default]"

    # Add iam.serviceAccountTokenCreator to allow K8s SA to impersonate GCP SA
    result = run_cmd(
        f'gcloud iam service-accounts add-iam-policy-binding {gcp_sa_email} --project={project} --member="{k8s_sa_member}" --role="roles/iam.serviceAccountTokenCreator"',
        check=False,
    )
    if result.returncode == 0:
        print(f"  roles/iam.serviceAccountTokenCreator granted to {k8s_sa_member}")
    elif "duplicate" in result.stderr.lower():
        print(f"  roles/iam.serviceAccountTokenCreator already exists for {k8s_sa_member}")
    else:
        print(f"  Warning: {result.stderr}")

    # Add iam.workloadIdentityUser to allow pods to use the GCP SA identity
    result = run_cmd(
        f'gcloud iam service-accounts add-iam-policy-binding {gcp_sa_email} --project={project} --member="{k8s_sa_member}" --role="roles/iam.workloadIdentityUser"',
        check=False,
    )
    if result.returncode == 0:
        print(f"  roles/iam.workloadIdentityUser granted to {k8s_sa_member}")
    elif "duplicate" in result.stderr.lower():
        print(f"  roles/iam.workloadIdentityUser already exists for {k8s_sa_member}")
    else:
        print(f"  Warning: {result.stderr}")

    return bucket_name


def add_node_sa_gcs_permissions(project):
    """Add GCS read/write permissions to node service account.

    The GKE node service account (default SA) needs storage.objectViewer
    and storage.objectCreator roles to access GCS buckets used by RAGFlow.

    Args:
        project: GCP project ID

    Note:
        Node SA typically has format: PROJECT_NUMBER-compute@developer.gserviceaccount.com
        The project number can be obtained from the project ID.
    """
    print("\n" + "=" * 70)
    print("Adding GCS Permissions to Node Service Account")
    print("=" * 70)

    # Get project number
    result = run_cmd(f"gcloud projects describe {project} --format='value(projectNumber)'", check=False)
    if result.returncode != 0:
        print(f"  Error: Could not get project number: {result.stderr}")
        return False

    project_number = result.stdout.strip()
    node_sa_email = f"{project_number}-compute@developer.gserviceaccount.com"

    print(f"\nNode service account: {node_sa_email}")

    # Grant storage.objectViewer (read) role
    print("\nGranting storage.objectViewer role...")
    result = run_cmd(
        f'gcloud projects add-iam-policy-binding {project} --member="serviceAccount:{node_sa_email}" --role="roles/storage.objectViewer"',
        check=False,
    )
    if result.returncode == 0:
        print("  storage.objectViewer role granted.")
    elif "already exists" in result.stderr or "duplicate" in result.stderr.lower():
        print("  storage.objectViewer role already granted.")
    else:
        print(f"  Warning: {result.stderr}")

    # Grant storage.objectCreator (write) role
    print("\nGranting storage.objectCreator role...")
    result = run_cmd(
        f'gcloud projects add-iam-policy-binding {project} --member="serviceAccount:{node_sa_email}" --role="roles/storage.objectCreator"',
        check=False,
    )
    if result.returncode == 0:
        print("  storage.objectCreator role granted.")
    elif "already exists" in result.stderr or "duplicate" in result.stderr.lower():
        print("  storage.objectCreator role already granted.")
    else:
        print(f"  Warning: {result.stderr}")

    print("\nNode service account now has GCS read/write permissions.")
    return True


def create_image_pull_secret(project, namespace="ragflow"):
    """Create imagePullSecret for GCR authentication.

    This solves the ImagePullBackOff issue when GKE kubelet requests GCR token
    with incorrect scope format when using Workload Identity.

    The solution uses a dedicated service account with explicit credentials
    via imagePullSecrets instead of relying on Workload Identity for GCR.

    Args:
        project: GCP project ID
        namespace: Kubernetes namespace (default: ragflow)

    Returns:
        True if secret was created or already exists, False on error
    """
    print("\n" + "=" * 70)
    print("Creating Image Pull Secret for GCR")
    print("=" * 70)

    image_sa_email = f"ragflow-image-pull@{project}.iam.gserviceaccount.com"
    secret_name = "gcr-image-pull"

    # Check if secret already exists
    print(f"\nChecking if imagePullSecret '{secret_name}' exists in namespace '{namespace}'...")
    result = run_cmd(f"kubectl get secret {secret_name} -n {namespace}", check=False)

    if result.returncode == 0:
        print(f"  Secret '{secret_name}' already exists.")
        return True

    # Step 1: Create service account for image pulling
    print(f"\nCreating service account '{image_sa_email}' for image pulling...")
    result = run_cmd(
        f'gcloud iam service-accounts create ragflow-image-pull --display-name="RAGFlow Image Pull" --description="Service account for pulling images from GCR" --project={project}',
        check=False,
    )
    if result.returncode == 0:
        print("  Service account created.")
    elif "already exists" in result.stderr:
        print("  Service account already exists.")
    else:
        print(f"  Warning: {result.stderr}")

    # Wait for SA to propagate
    print("\nWaiting for service account to propagate...")
    import time

    time.sleep(5)

    # Step 2: Grant artifactregistry.reader role
    print(f"\nGranting artifactregistry.reader role to {image_sa_email}...")
    result = run_cmd(
        f'gcloud projects add-iam-policy-binding {project} --member="serviceAccount:{image_sa_email}" --role="roles/artifactregistry.reader"',
        check=False,
    )
    if result.returncode == 0:
        print("  Role granted successfully.")
    elif "already exists" in result.stderr or "duplicate" in result.stderr.lower():
        print("  Role already granted.")
    else:
        print(f"  Warning: {result.stderr}")

    # Step 3: Create SA key for imagePullSecret
    print("\nCreating service account key...")
    key_file = "/tmp/image-pull-key.json"
    result = run_cmd(
        f"gcloud iam service-accounts keys create {key_file} --iam-account={image_sa_email}",
        check=False,
    )
    if result.returncode != 0:
        print(f"  Error: Could not create SA key: {result.stderr}")
        return False

    # Step 4: Create imagePullSecret
    print(f"\nCreating imagePullSecret '{secret_name}' in namespace '{namespace}'...")
    with open(key_file, "r") as f:
        key_content = f.read()

    # Use kubectl to create the secret
    cmd = f"kubectl create secret docker-registry {secret_name} --docker-server=gcr.io --docker-username=_json_key --docker-password='{key_content}' --docker-email={image_sa_email} -n {namespace}"
    result = run_cmd(cmd, check=False)

    # Clean up key file
    try:
        os.remove(key_file)
    except OSError:
        pass

    if result.returncode == 0:
        print(f"  imagePullSecret '{secret_name}' created successfully!")
        print("\n  Add to your deployment spec:")
        print("    image_pull_secrets:")
        print(f"      - name: {secret_name}")
        return True
    else:
        print(f"  Error: Could not create secret: {result.stderr}")
        return False


def create_notification_channel(project, email_address):
    """Create email notification channel for alerts (idempotent).

    This function is idempotent - if a notification channel with the given
    email address already exists, it will return the existing channel ID
    instead of creating a duplicate.

    Args:
        project: GCP project ID
        email_address: Email address for notifications

    Returns:
        Channel ID (resource name) if created successfully, None otherwise
    """
    # Create unique channel name based on email (use username part to avoid duplicates)
    email_username = email_address.split("@")[0] if "@" in email_address else email_address
    channel_name = f"ragflow-alert-{email_username}"

    print("\n" + "=" * 70)
    print("Creating Notification Channel")
    print("=" * 70)

    # Check if channel already exists by email address label.
    # The filter value must be quoted, otherwise gcloud does not match the label reliably.
    result = run_cmd(
        f"gcloud alpha monitoring channels list --project={project} --filter='labels.email_address=\"{email_address}\"' --format=json",
        check=False,
    )

    if result.returncode == 0:
        try:
            channels = json.loads(result.stdout)
            if channels:
                # Return the full resource name
                channel_id = channels[0].get("name", "")
                print(f"  Notification channel for {email_address} already exists: {channel_id}")
                return channel_id
        except (json.JSONDecodeError, KeyError, IndexError):
            pass

    # Create the channel
    print(f"  Creating email notification channel for {email_address}...")
    result = run_cmd(
        f'gcloud alpha monitoring channels create --display-name="{channel_name}" --type=email --channel-labels=email_address={email_address} --project={project}',
        check=False,
    )

    if result.returncode == 0:
        # Extract channel ID from output
        output = result.stdout
        if "name:" in output:
            channel_id = output.split("name:")[1].strip().split("\n")[0]
            print(f"  Notification channel created: {channel_id}")
            return channel_id
        # Try to get from list by email
        result = run_cmd(
            f"gcloud alpha monitoring channels list --project={project} --filter='labels.email_address=\"{email_address}\"' --format=json",
            check=False,
        )
        if result.returncode == 0:
            channels = json.loads(result.stdout)
            if channels:
                channel_id = channels[0].get("name", "")
                return channel_id
    else:
        print(f"  Warning: Could not create notification channel: {result.stderr}")

    return None


def setup_monitoring_alerts(project, cluster_name, region, zone=None):
    """Setup monitoring alerts for Pod status monitoring using PromQL.

    Uses conditionPrometheusQueryLanguage for PromQL-based alerting with GKE metrics.

    Args:
        project: GCP project ID
        cluster_name: GKE cluster name
        region: GKE cluster region
        zone: GKE cluster zone (optional)

    Returns:
        True if setup was successful, False otherwise

    Note:
        This function creates:
        - Alert policies using PromQL queries against GKE metrics via GMP
        - Email notification channel(s) (if GCP_ALERT_EMAIL is set)

    Environment Variables:
        GCP_ALERT_EMAIL: Comma-separated list of email addresses
    """
    email_config = os.environ.get("GCP_ALERT_EMAIL")
    if not email_config:
        print("\nSkipping alert setup: GCP_ALERT_EMAIL not set")
        return False

    # Support comma-separated multiple email addresses
    email_addresses = [e.strip() for e in email_config.split(",") if e.strip()]
    if not email_addresses:
        print("\nSkipping alert setup: GCP_ALERT_EMAIL is empty")
        return False

    print("\n" + "=" * 70)
    print("Setting Up Monitoring Alerts (PromQL)")
    print("=" * 70)
    print(f"  Alert emails: {', '.join(email_addresses)}")
    print("  Using PromQL for state-based alerting")

    # Create notification channels for each email
    channel_ids = []
    for email in email_addresses:
        channel_id = create_notification_channel(project, email)
        if channel_id:
            channel_ids.append(channel_id)

    return create_gmp_alert_policy(project, channel_ids, cluster_name, region, zone)


def create_gmp_alert_policy(project, channel_ids, cluster_name, region, zone=None):
    """Create alert policies for Pod status monitoring using PromQL (idempotent).

    Uses conditionPrometheusQueryLanguage to support PromQL queries including
    increase(), rate(), and other functions for proper state-based alerting.

    This function is idempotent - if policies already exist, it will update notification
    channels and return True without creating duplicates.

    Args:
        project: GCP project ID
        channel_ids: List of notification channel IDs (resource names)
        cluster_name: GKE cluster name
        region: GKE cluster region
        zone: GKE cluster zone (optional, uses region if not provided)

    Returns:
        True if created or updated successfully
    """
    print("\n" + "=" * 70)
    print("Creating Alert Policies (PromQL)")
    print("=" * 70)

    # Enable Managed Prometheus on the cluster first
    enable_managed_prometheus(project, cluster_name, region, zone)

    # Define alert policies using conditionPrometheusQueryLanguage (PromQL) for
    # Prometheus metrics or conditionThreshold for Anthos metrics.
    #
    # Note: GCP Managed Prometheus only exposes a subset of kube-state-metrics.
    # For container restart metrics, we must use Anthos metrics
    # (kubernetes.io/anthos/kube_pod_container_status_restarts_total) with
    # conditionThreshold, as they are not available via Prometheus.
    policies = [
        {
            "display_name": f"Critical - Pod Not Running ({cluster_name})",
            "description": "At least one pod is not in Running phase - indicates crash, eviction, or scheduling failure",
            "condition_type": "promql",
            "promql": f'kube_pod_status_phase{{cluster="{cluster_name}",namespace="ragflow",phase!="Running","pod"!~"ohttps-cert-sync-*"}} == 1',
            "duration": "120s",
            "severity": "CRITICAL",
        },
        {
            "display_name": f"Critical - Container Restarting ({cluster_name})",
            "description": "Containers in ragflow namespace are restarting - indicates unstable workload or resource issues",
            "condition_type": "threshold",
            "metric_type": "kubernetes.io/anthos/kube_pod_container_status_restarts_total",
            "filter_template": 'resource.type="k8s_container" AND metric.type="kubernetes.io/anthos/kube_pod_container_status_restarts_total" AND resource.labels.cluster_name="{cluster}" AND resource.labels.namespace_name="ragflow"',
            "comparison": "COMPARISON_GT",
            "threshold_value": 0,
            "duration": "60s",
            # Required for CUMULIVE metrics - ALIGN_RATE computes the rate of change
            "aggregation": {
                "alignmentPeriod": "60s",
                "perSeriesAligner": "ALIGN_RATE",
            },
            "severity": "CRITICAL",
        },
    ]

    all_created = True

    for policy_def in policies:
        policy_display_name = policy_def["display_name"]

        def policy_exists():
            verify_result = run_cmd(
                f'gcloud alpha monitoring policies list --project={project} --filter="displayName=\'{policy_display_name}\'" --format="value(name)"',
                check=False,
            )
            if verify_result.returncode != 0:
                return False, verify_result.stderr, ""
            policy_name = verify_result.stdout.strip()
            policy_id = policy_name.split("/")[-1] if policy_name else ""
            return bool(policy_name), policy_name, policy_id

        def update_existing_policy(policy_name, policy_id):
            if not channel_ids:
                print(f"  Alert policy '{policy_display_name}' already exists, but no notification channels.")
                return True

            channel_arg = ",".join(channel_ids)
            result = run_cmd(
                f"gcloud alpha monitoring policies update {policy_name} --project={project} --set-notification-channels={channel_arg}",
                check=False,
            )
            if result.returncode == 0:
                print(f"  Alert policy '{policy_display_name}' already exists. Notification channels refreshed.")
                print(f"  Manage at: https://console.cloud.google.com/monitoring/alerting/policies/{policy_id}?project={project}")
                return True
            print(f"  Warning: Policy exists but channel update failed: {result.stderr}")
            return False

        # Check if policy already exists
        exists, existing_policy, existing_policy_id = policy_exists()
        if exists:
            update_existing_policy(existing_policy, existing_policy_id)
            continue

        # Build condition based on type: promql or threshold
        if policy_def["condition_type"] == "promql":
            condition = {
                "displayName": policy_def["description"],
                "conditionPrometheusQueryLanguage": {
                    "query": policy_def["promql"],
                    "duration": policy_def["duration"],
                },
            }
        else:  # threshold
            threshold_cond = {
                "filter": policy_def["filter_template"].format(cluster=cluster_name),
                "comparison": policy_def["comparison"],
                "thresholdValue": policy_def["threshold_value"],
                "duration": policy_def["duration"],
            }
            if "aggregation" in policy_def:
                threshold_cond["aggregations"] = [policy_def["aggregation"]]
            condition = {
                "displayName": policy_def["description"],
                "conditionThreshold": threshold_cond,
            }

        # Build policy JSON for alert policies
        policy_json = {
            "displayName": policy_display_name,
            "combiner": "OR",
            "conditions": [condition],
            "notificationChannels": channel_ids,
            "enabled": True,
            "severity": policy_def.get("severity", "CRITICAL"),
        }

        if not channel_ids:
            print(f"  Warning: No notification channels provided for '{policy_display_name}'.")

        fd, policy_file = tempfile.mkstemp(suffix=".json", text=True)
        try:
            with os.fdopen(fd, "w") as f:
                json.dump(policy_json, f, indent=2)

            print(f"  Creating alert policy '{policy_display_name}'...")
            result = run_cmd(
                f"gcloud alpha monitoring policies create --policy-from-file={policy_file} --project={project}",
                check=False,
            )

            if result.returncode == 0:
                verified, verify_name, verify_id = policy_exists()
                if verified:
                    print("  Alert policy created successfully!")
                    print(f"  Manage at: https://console.cloud.google.com/monitoring/alerting/policies/{verify_id}?project={project}")
                else:
                    print("  Warning: Create succeeded but verification failed.")
                    all_created = False
            else:
                print(f"  Warning: Could not create alert policy: {result.stderr}")
                all_created = False
        finally:
            if os.path.exists(policy_file):
                os.remove(policy_file)

    return all_created


def main():
    print("=" * 70)
    print("GKE Cluster Configuration Helper")
    print("=" * 70)

    # Check kubectl installed
    check_kubectl_installed()

    # Check gcloud installed
    check_gcloud_installed()

    # Configure docker for GCR authentication (non-interactive)
    configure_docker_for_gcr()

    # Check gke-auth-plugin
    check_gke_auth_plugin()

    # List and select cluster
    cluster, project = list_gke_clusters()

    # Get cluster info
    print(f"\nConnecting to cluster: {cluster['name']}")
    endpoint, cluster_ca_cert, region, zone = get_cluster_info(cluster, project)
    print(f"  Endpoint: {endpoint}")
    print(f"  Region: {region}")

    # Enable public access
    enable_public_access(project, cluster["name"], region, zone)

    # Enable Gateway API
    enable_gateway_api(project, cluster["name"], region, zone)

    # Enable Managed Prometheus for GKE monitoring (required for PodMonitor resources)
    enable_managed_prometheus(project, cluster["name"], region, zone)

    # Create proxy-only subnet for GKE Gateway
    create_proxy_only_subnet(project, cluster["name"], region)

    # Create kubeconfig
    create_kubeconfig(endpoint, cluster_ca_cert, cluster["name"], project)

    # Read namespace from terraform.tfvars (needed for GCS bucket setup)
    tfvars_path = "terraform.tfvars"
    namespace = "ragflow"  # default
    if os.path.exists(tfvars_path):
        with open(tfvars_path, "r") as f:
            for line in f:
                if line.strip().startswith("namespace"):
                    namespace = line.split("=")[1].strip().strip('"')
                    break

    # Create GCS bucket for RAGFlow storage (includes bucket IAM and Workload Identity setup)
    bucket_name = create_gcs_bucket(project, cluster.get("id", cluster.get("name", "cluster")), region, namespace)

    # Update terraform.tfvars with all variables at once
    tfvars_vars = {
        "private_registry": f"{GCR_PROJECT_PREFIX}/{project}",
        "gcp_project_id": project,
    }
    if bucket_name:
        tfvars_vars["s3_bucket"] = bucket_name

    example_file = "terraform.tfvars.dev_gke"
    update_terraform_tfvars(tfvars_vars, example_file)

    # Create imagePullSecret for GCR (solves ImagePullBackOff issue)
    create_image_pull_secret(project, namespace)

    # Setup monitoring alerts (only if GCP_ALERT_EMAIL is set)
    if os.environ.get("GCP_ALERT_EMAIL"):
        if not setup_monitoring_alerts(project, cluster["name"], region, zone):
            print("\nError: Failed to setup monitoring alerts. Exiting.")
            sys.exit(1)


if __name__ == "__main__":
    # Check for -h flag before running main
    if len(sys.argv) > 1 and (sys.argv[1] == "-h" or sys.argv[1] == "--help"):
        print_help()
        sys.exit(0)
    main()
