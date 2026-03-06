#!/usr/bin/env python3
"""
GKE Cluster Configuration Helper Script

This script configures GKE cluster access, enables Gateway API, creates
GCS service account with Workload Identity, and generates the necessary
configuration for RAGFlow deployment.

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
    python3 setup_gke_config.py [-h]
"""

import os
import sys
import subprocess
import json
import shutil


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
    run_cmd("sudo apt-get update -qq", check=False)
    result = run_cmd("sudo apt-get install -y kubectl", check=False)

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


def run_cmd(cmd, check=True, capture_output=True):
    """Run a shell command and return the result.

    Args:
        cmd: A shell command string to be executed with shell=True
        check: If True, exit on command failure; if False, silently handle errors
        capture_output: If True, capture stdout and stderr
    """
    # Always use shell=True for string commands
    result = subprocess.run(cmd, capture_output=capture_output, text=True, check=False, shell=True)
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
        # Try alternative CIDR if the default one fails
        print("  First attempt failed. Trying alternative CIDR range...")

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


def create_gcs_bucket(project, cluster_id):
    """Create GCS bucket for RAGFlow storage (if not exists)."""
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
    result = run_cmd(f"gsutil ls -b gs://{bucket_name} 2>/dev/null", check=False)

    if result.returncode == 0:
        print(f"  Bucket '{bucket_name}' already exists.")
        return bucket_name

    # Create bucket
    print(f"  Bucket not found. Creating '{bucket_name}'...")
    result = run_cmd(f"gsutil mb -p {project} -l us-central1 gs://{bucket_name}/", check=False)

    if result.returncode == 0:
        print(f"  Bucket '{bucket_name}' created successfully!")
    else:
        print(f"  Error: Could not create bucket: {result.stderr}")
        sys.exit(1)

    return bucket_name


def create_gcs_service_account(project, cluster_name, region, namespace="ragflow"):
    """Create GCS service account and configure Workload Identity."""
    print("\n" + "=" * 70)
    print("Creating GCS Service Account with Storage Admin Role")
    print("=" * 70)

    gcs_sa_email = f"ragflow-gcs@{project}.iam.gserviceaccount.com"
    k8s_sa_name = "ragflow-gcs"  # Kubernetes Service Account name

    # Check if service account already exists
    print(f"\nChecking if service account '{gcs_sa_email}' exists...")
    result = run_cmd(f"gcloud iam service-accounts describe {gcs_sa_email} --project={project}", check=False)

    if result.returncode != 0:
        print("  Service account not found. Creating...")
        # Create service account
        result = run_cmd(f'gcloud iam service-accounts create ragflow-gcs --display-name="RAGFlow GCS Service Account" --description="Service account for RAGFlow to access GCS" --project={project}')
        print("  Service account created successfully!")
    else:
        print("  Service account already exists.")

    # Grant Storage Admin role (includes bucket listing and object operations)
    print(f"\nGranting Storage Admin role to {gcs_sa_email}...")
    result = run_cmd(f'gcloud projects add-iam-policy-binding {project} --member="serviceAccount:{gcs_sa_email}" --role="roles/storage.admin" --condition=None', check=False)

    if result.returncode == 0:
        print("  Storage Admin role granted successfully!")
    else:
        # Check if already has the role
        check_result = run_cmd(f"gcloud projects get-iam-policy {project} --format=json", check=False)
        if check_result.returncode == 0:
            import json

            try:
                policy = json.loads(check_result.stdout)
                for binding in policy.get("bindings", []):
                    if binding.get("role") == "roles/storage.admin":
                        members = binding.get("members", [])
                        if f"serviceAccount:{gcs_sa_email}" in members:
                            print("  Service account already has Storage Object Admin role.")
                            return
            except Exception:
                pass
        print(f"  Warning: Could not grant role (may already exist): {result.stderr}")

    # Configure Workload Identity
    print("\nConfiguring Workload Identity...")
    print(f"  GCP Service Account: {gcs_sa_email}")
    print(f"  Kubernetes Namespace: {namespace}")
    print(f"  Kubernetes SA Name: {k8s_sa_name}")

    # Create K8s Service Account if not exists
    print("\n  Creating Kubernetes Service Account...")
    result = run_cmd(f"kubectl get serviceaccount {k8s_sa_name} -n {namespace}", check=False)

    if result.returncode != 0:
        run_cmd(f"kubectl create serviceaccount {k8s_sa_name} -n {namespace}", check=False)
        print(f"    Created K8s SA: {k8s_sa_name}")
    else:
        print(f"    K8s SA already exists: {k8s_sa_name}")

    # Bind GCP SA to K8s SA using IAM
    print("\n  Binding GCP SA to K8s SA (IAM)...")
    member = f"serviceAccount:{project}.svc.id.goog[{namespace}/{k8s_sa_name}]"
    result = run_cmd(f'gcloud iam service-accounts add-iam-policy-binding {gcs_sa_email} --member="{member}" --role=roles/iam.workloadIdentityUser --project={project}', check=False)

    if result.returncode == 0:
        print("    IAM binding created successfully!")
    else:
        # Check if already bound
        if "already exists" in result.stderr or result.returncode == 0:
            print("    IAM binding already exists.")
        else:
            print(f"    Warning: {result.stderr}")

    # Annotate K8s SA with GCP SA email
    print("\n  Annotating Kubernetes Service Account...")
    run_cmd(f"kubectl annotate serviceaccount {k8s_sa_name} -n {namespace} iam.gke.io/gcp-service-account={gcs_sa_email} --overwrite", check=False)
    print(f"    Annotation added: iam.gke.io/gcp-service-account={gcs_sa_email}")

    print("\n  Workload Identity configured successfully!")
    print(f"  Use this K8s SA in your deployment: serviceAccountName: {k8s_sa_name}")


def print_next_steps():
    """Print next steps for the user."""
    print("\n" + "=" * 70)
    print("NEXT STEPS")
    print("=" * 70)
    print("""
Note: The setup_gke_config.py script has already:
  1. Enabled public access to the GKE cluster
  2. Enabled Gateway API on the cluster
  3. Created a proxy-only subnet for GKE Gateway

If running setup_gke_config.py again:
  - The proxy-only subnet will be created if it doesn't exist
  - If it already exists with the correct purpose, it will be skipped

If gke-gcloud-auth-plugin is not available, install via apt:
  echo 'deb [signed-by=/usr/share/keyrings/cloud.google.gpg] https://packages.cloud.google.com/apt cloud-sdk main' | sudo tee /etc/apt/sources.list.d/google-cloud-sdk.list
  curl -fsSL https://packages.cloud.google.com/apt/doc/apt-key.gpg | sudo gpg --dearmor -o /usr/share/keyrings/cloud.google.gpg
  sudo apt-get update && sudo apt-get install -y google-cloud-cli-gke-gcloud-auth-plugin

Manual proxy-only subnet creation (if needed):
  gcloud compute networks subnets create proxy-only-subnet-asia-east2 \\
    --network=default \\
    --region=asia-east2 \\
    --range=10.120.0.0/23 \\
    --purpose=REGIONAL_MANAGED_PROXY \\
    --role=ACTIVE

1. Pull RAGFlow images from Aliyun registry and push to GCR:

   # Login to Aliyun registry (use your Aliyun credentials)
   sudo docker login infiniflow-registry.cn-shanghai.cr.aliyuncs.com

   # Pull images
   sudo docker pull infiniflow-registry.cn-shanghai.cr.aliyuncs.com/infiniflow-ai/ragflow:latest
   sudo docker pull infiniflow-registry.cn-shanghai.cr.aliyuncs.com/infiniflow-ai/deepdoc_cpu:latest

   # Tag for GCR
   sudo docker tag infiniflow-registry.cn-shanghai.cr.aliyuncs.com/infiniflow-ai/ragflow:latest gcr.io/{PROJECT}/ragflow:latest
   sudo docker tag infiniflow-registry.cn-shanghai.cr.aliyuncs.com/infiniflow-ai/deepdoc_cpu:latest gcr.io/{PROJECT}/deepdoc_cpu:latest

   # Login to GCR
   gcloud auth configure-docker

   # Push to GCR
   sudo docker push gcr.io/{PROJECT}/ragflow:latest
   sudo docker push gcr.io/{PROJECT}/deepdoc_cpu:latest

2. Deploy RAGFlow with OpenTofu:

   cd onpremises
   tofu init
   tofu plan
   tofu apply

3. Access RAGFlow:

   # Get the gateway IP
   kubectl get svc -n ragflow

   # Or use port-forward for testing
   kubectl port-forward -n ragflow svc/ragflow 9380:9380
   # Then access http://localhost:9380
""")
    print("=" * 70)


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

    # Create proxy-only subnet for GKE Gateway
    create_proxy_only_subnet(project, cluster["name"], region)

    # Create kubeconfig
    create_kubeconfig(endpoint, cluster_ca_cert, cluster["name"], project)

    # Create GCS bucket for RAGFlow storage
    bucket_name = create_gcs_bucket(project, cluster.get("id", cluster.get("name", "cluster")))

    # Update terraform.tfvars with all variables at once
    tfvars_vars = {
        "private_registry": f"{GCR_PROJECT_PREFIX}/{project}",
        "gcp_project_id": project,
    }
    if bucket_name:
        tfvars_vars["s3_bucket"] = bucket_name

    example_file = "terraform.tfvars.dev_gke"
    update_terraform_tfvars(tfvars_vars, example_file)

    # Print GCS service account info
    gcs_sa = f"ragflow-gcs@{project}.iam.gserviceaccount.com"
    print(f"  GCS service account: {gcs_sa}")

    # Read namespace from terraform.tfvars
    tfvars_path = "terraform.tfvars"
    namespace = "ragflow"  # default
    if os.path.exists(tfvars_path):
        with open(tfvars_path, "r") as f:
            for line in f:
                if line.strip().startswith("namespace"):
                    namespace = line.split("=")[1].strip().strip('"')
                    break

    # Create GCS service account and configure Workload Identity
    create_gcs_service_account(project, cluster["name"], region, namespace)

    # Print next steps
    print_next_steps()


if __name__ == "__main__":
    # Check for -h flag before running main
    if len(sys.argv) > 1 and (sys.argv[1] == "-h" or sys.argv[1] == "--help"):
        print_help()
        sys.exit(0)
    main()
