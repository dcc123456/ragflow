#!/usr/bin/env python3
"""
GKE Backup and Restore

This script manages GKE backup and restore operations with subcommands:
  - plan:  List backup plans, backups, and restore plans with Google Cloud console links
  - apply: Create/update backup plan, trigger backup, create/update restore plan, execute restore

Usage:
    python3 gke_backup_restore.py plan SOURCE_PROJECT.CLUSTER DESTINATION_PROJECT.CLUSTER
    python3 gke_backup_restore.py apply SOURCE_PROJECT.CLUSTER DESTINATION_PROJECT.CLUSTER

Example (cross-project):
    python3 gke_backup_restore.py plan prod-project.prod-cluster-1 stage-project.stage-cluster-1
    python3 gke_backup_restore.py apply prod-project.prod-cluster-1 stage-project.stage-cluster-1

Example (same-project):
    python3 gke_backup_restore.py plan my-project.cluster-1 my-project.cluster-2
    python3 gke_backup_restore.py apply my-project.cluster-1 my-project.cluster-2

Prerequisites:
    - gcloud CLI installed and authenticated with an account that has required IAM roles (see below)
    - Required APIs enabled (gkebackup.googleapis.com) on source project and destination project (for cross-project)
    - Script performs pre-flight check before executing; if check fails, see error messages for resolution

Required IAM Roles (for the authenticated gcloud account):
    - Source Project: gkebackup.backupAdmin (for backup plans and backups)
    - Destination Project: gkebackup.restoreAdmin, gkebackup.backupAdmin (for cross-project restore)
    - Note: For cross-project, the script automatically creates a service account and grants roles for backup/restore operations
"""

import argparse
import json
import subprocess
import sys
import time
from typing import Optional, Tuple


def run_cmd(cmd: list, check: bool = True, capture_output: bool = True) -> subprocess.CompletedProcess:
    """Run a shell command and return the result."""
    result = subprocess.run(
        cmd,
        capture_output=capture_output,
        text=True,
        check=False,
        shell=isinstance(cmd, str)
    )
    if check and result.returncode != 0:
        print(f"Error running command: {' '.join(cmd)}")
        print(f"stderr: {result.stderr}")
        sys.exit(1)
    return result


def config_matches(expected: dict, actual: dict, path: str = "") -> bool:
    """Check if actual config matches expected, comparing only fields in expected.

    Handles nested dicts and ignores server-side generated extra fields.
    """
    for key, expected_value in expected.items():
        actual_value = actual.get(key)
        if isinstance(expected_value, dict):
            if not isinstance(actual_value, dict):
                return False
            if not config_matches(expected_value, actual_value, f"{path}.{key}"):
                return False
        elif actual_value != expected_value:
            return False
    return True


def check_prerequisites(projects: list) -> bool:
    """Check gcloud CLI prerequisites before running operations.

    Args:
        projects: List of projects that will be accessed

    Returns:
        True if all checks pass
    """
    errors = []

    # Check 1: gcloud CLI is installed
    print("Checking prerequisites...")
    result = run_cmd(["gcloud", "version"], check=False)
    if result.returncode != 0:
        errors.append("gcloud CLI is not installed or not in PATH. Install: https://cloud.google.com/sdk/docs/install")
        return False

    # Check 2: User is authenticated
    result = run_cmd(["gcloud", "auth", "list", "--format=json"], check=False)
    if result.returncode != 0:
        errors.append("Not authenticated. Run: gcloud auth login")
        return False

    try:
        accounts = json.loads(result.stdout)
        active_accounts = [a for a in accounts if a.get("status") == "ACTIVE"]
        if not active_accounts:
            errors.append("No active account. Run: gcloud auth login")
        else:
            print(f"  [OK] Authenticated as: {active_accounts[0].get('account', 'unknown')}")
    except json.JSONDecodeError:
        errors.append("Failed to parse auth info. Run: gcloud auth login")

    if errors:
        for e in errors:
            print(f"  [FAIL] {e}")
        return False

    return True


def enable_api(project: str, api: str = "gkebackup.googleapis.com") -> bool:
    """Enable an API in the specified project."""
    print(f"Enabling {api} in project {project}...")
    result = run_cmd(
        ["gcloud", "services", "enable", api, "--project", project],
        check=False
    )
    if result.returncode == 0:
        print(f"  [OK] {api} enabled")
        return True
    else:
        if "already enabled" in result.stderr.lower() or "already has been enabled" in result.stderr.lower():
            print(f"  [OK] {api} already enabled")
            return True
        print(f"  [FAIL] Failed to enable {api}: {result.stderr}")
        return False


def get_cluster_info(project: str, cluster_name: str) -> Optional[Tuple[str, str]]:
    """Get cluster location and full details.

    Returns:
        Tuple of (location, cluster_path) or None if not found.
    """
    result = run_cmd(
        ["gcloud", "container", "clusters", "list", "--project", project, "--format", "json"],
        check=False
    )

    if result.returncode != 0:
        return None

    try:
        clusters = json.loads(result.stdout)
    except json.JSONDecodeError:
        return None

    for c in clusters:
        if c.get("name") == cluster_name:
            location = c.get("location", "")
            cluster_path = f"projects/{project}/locations/{location}/clusters/{cluster_name}"
            return location, cluster_path

    return None


def list_clusters(project: str) -> list:
    """List all GKE clusters in the project."""
    result = run_cmd(
        ["gcloud", "container", "clusters", "list", "--project", project, "--format", "json"],
        check=False
    )

    if result.returncode != 0:
        print(f"  [FAIL] Failed to list clusters: {result.stderr}")
        return []

    try:
        clusters = json.loads(result.stdout)
        return clusters
    except json.JSONDecodeError:
        return []


def create_service_account(project: str, name: str = "restore-sa") -> str:
    """Create a service account for restore operations."""
    sa_email = f"{name}@{project}.iam.gserviceaccount.com"

    print(f"\nCreating service account {sa_email}...")

    # Check if SA already exists
    result = run_cmd(
        ["gcloud", "iam", "service-accounts", "describe", sa_email, "--project", project],
        check=False
    )

    if result.returncode == 0:
        print("  [OK] Service account already exists")
        return sa_email

    # Create SA
    result = run_cmd(
        [
            "gcloud", "iam", "service-accounts", "create", name,
            "--display-name", "GKE Backup Restore SA",
            "--project", project
        ],
        check=False
    )

    if result.returncode == 0:
        print("  [OK] Service account created")
        return sa_email
    else:
        print(f"  [FAIL] Failed to create service account: {result.stderr}")
        return sa_email


def grant_iam_role(project: str, member: str, role: str) -> bool:
    """Grant an IAM role to a member."""
    print(f"  Granting {role} to {member}...")

    result = run_cmd(
        [
            "gcloud", "projects", "add-iam-policy-binding", project,
            "--member", f"serviceAccount:{member}",
            "--role", role
        ],
        check=False
    )

    if result.returncode == 0:
        print("    [OK] Role granted")
        return True
    else:
        if "already has" in result.stderr.lower():
            print("    [OK] Role already assigned")
            return True
        print(f"    [FAIL] Failed to grant role: {result.stderr}")
        return False


def create_restore_channel(
    source_project: str,
    location: str,
    destination_project: str,
    channel_name: str = "restore-channel"
) -> Tuple[bool, str]:
    """Create a restore channel in the source project."""

    # Check if channel already exists
    result = run_cmd(
        [
            "gcloud", "beta", "container", "backup-restore", "restore-channels", "list",
            "--project", source_project,
            "--format", "json"
        ],
        check=False
    )

    if result.returncode == 0:
        try:
            channels = json.loads(result.stdout)
            for ch in channels:
                if channel_name in ch.get("name", ""):
                    print(f"\nRestore channel '{channel_name}' already exists")
                    print(f"  [OK] Reusing existing channel: {ch['name']}")
                    return True, ch['name']
        except json.JSONDecodeError:
            pass

    print(f"\nCreating restore channel in {source_project}...")

    result = run_cmd(
        [
            "gcloud", "beta", "container", "backup-restore", "restore-channels", "create",
            channel_name,
            "--project", source_project,
            "--location", location,
            "--destination-project", f"projects/{destination_project}",
            "--description", f"Restore channel to {destination_project}"
        ],
        check=False
    )

    if result.returncode == 0:
        channel_full_name = f"projects/{source_project}/locations/{location}/restoreChannels/{channel_name}"
        print(f"  [OK] Restore channel created: {channel_full_name}")
        return True, channel_full_name
    else:
        print(f"  [FAIL] Failed to create restore channel: {result.stderr}")
        return False, ""


def get_restore_plan_config(destination_project: str, location: str, restore_plan_name: str) -> Optional[dict]:
    """Get restore plan configuration."""
    result = run_cmd(
        [
            "gcloud", "beta", "container", "backup-restore", "restore-plans", "describe",
            restore_plan_name,
            "--project", destination_project,
            "--location", location,
            "--format", "json"
        ],
        check=False
    )

    if result.returncode == 0:
        try:
            return json.loads(result.stdout)
        except json.JSONDecodeError:
            return None
    return None


def get_backup_plan_by_name(project: str, location: str, backup_plan_name: str) -> Optional[dict]:
    """Get backup plan info by name."""
    result = run_cmd(
        [
            "gcloud", "beta", "container", "backup-restore", "backup-plans", "describe",
            backup_plan_name,
            "--project", project,
            "--location", location,
            "--format", "json"
        ],
        check=False
    )

    if result.returncode == 0:
        try:
            return json.loads(result.stdout)
        except json.JSONDecodeError:
            return None
    return None


def list_backup_plans(project: str) -> list:
    """List all backup plans in project."""
    result = run_cmd(
        [
            "gcloud", "beta", "container", "backup-restore", "backup-plans", "list",
            "--project", project,
            "--format", "json"
        ],
        check=False
    )

    if result.returncode == 0:
        try:
            return json.loads(result.stdout)
        except json.JSONDecodeError:
            return []
    return []


def list_backups(project: str, location: str, backup_plan_name: str) -> list:
    """List all backups under a backup plan."""
    result = run_cmd(
        [
            "gcloud", "beta", "container", "backup-restore", "backups", "list",
            "--project", project,
            "--location", location,
            "--backup-plan", backup_plan_name,
            "--format", "json"
        ],
        check=False
    )

    if result.returncode == 0:
        try:
            return json.loads(result.stdout)
        except json.JSONDecodeError:
            return []
    return []


def list_restore_plans(project: str) -> list:
    """List all restore plans in project."""
    result = run_cmd(
        [
            "gcloud", "beta", "container", "backup-restore", "restore-plans", "list",
            "--project", project,
            "--format", "json"
        ],
        check=False
    )

    if result.returncode == 0:
        try:
            return json.loads(result.stdout)
        except json.JSONDecodeError:
            return []
    return []


def get_console_url(resource_type: str, project: str, location: str = None, resource_name: str = None) -> str:
    """Generate Google Cloud Console URL for GKE Backup resources.

    Returns base backup console URL - users can navigate to specific resources from there.
    """
    if resource_type == "backupPlans":
        return f"https://console.cloud.google.com/kubernetes/backups/backupPlans?project={project}"
    elif resource_type == "backups":
        return f"https://console.cloud.google.com/kubernetes/backups/backupPlans?project={project}"
    elif resource_type == "restorePlans":
        return f"https://console.cloud.google.com/kubernetes/backups/restorePlans?project={project}"
    else:
        return f"https://console.cloud.google.com/kubernetes/backups?project={project}"


# Exclusion filter for cross-project restore.
# Filter format: https://docs.cloud.google.com/kubernetes-engine/docs/add-on/backup-for-gke/how-to/fine-grained-restore
#
# IMPORTANT NOTES about GKE Backup Restore fine-grained filter limitations:
#
# 1. labels is a simple map[string]string, NOT matchExpressions
#    gcloud's Fine-grained Restore Filter does NOT support Kubernetes-style matchExpressions
#    (i.e., operator=In/NotIn with a values array). In gcloud backup-restore config files,
#    labels is defined as map[string]string -- the value MUST be a string, NOT a list.
#    Wrong:  values: [admin, deepdoc, parser]  (JSON array, will cause type error)
#    Right:  values:                           (one rule per label value, see below)
#
# 2. To match multiple values (OR logic), define multiple filter entries
#    Since operator+values is not supported, if you want to exclude resources where
#    label app IN (admin, deepdoc, parser, ragflow), you MUST write four separate rules,
#    one for each value. See loop over EXCLUDE_APP_LABELS below.
#
# 3. resourceGroup:
#    Kubernetes categorizes resources by "API Group". The core resources
#    (Pod, Service, Namespace, ConfigMap, etc.) have NO group name in their API path
#    (e.g., /api/v1/pods). In GKE Backup YAML, these use resourceGroup: "".
#    Non-core resources use their API group name:
#      Deployment, ReplicaSet -> apps
#      Ingress                -> networking.k8s.io
#      Pod                    -> "" (empty string, core/v1 group)
#
EXCLUDE_APP_LABELS = ["admin", "deepdoc", "parser", "ragflow"]

# Cluster-scoped Cilium kinds that must be excluded from same-project restore
# because they already exist on the cluster and conflict on restore.
# In cross-project restore these are also excluded since Cilium identity/node
# state is cluster-specific and cannot be migrated between clusters.
EXCLUDE_CILIUM_KINDS = ["CiliumIdentity", "CiliumNode"]

# Gateway API resources should never be restored into an existing cluster.
# They are managed by the current cluster's ingress/gateway control plane and
# restoring backup versions can corrupt the live Gateway controller state.
EXCLUDE_GATEWAY_API_GROUP = "gateway.networking.k8s.io"
EXCLUDE_GATEWAY_API_KINDS = [
    "Gateway",
    "HTTPRoute",
    "GatewayClass",
    "BackendTLSPolicy",
    "ReferenceGrant",
]

# Restore order: forces Secret and ConfigMap to be restored BEFORE Workloads.
# Without this, workloads may start before their Secrets/ConfigMaps exist,
# causing "secret not found" errors and Workload Validation timeout.
#
# Docs: https://docs.cloud.google.com/kubernetes-engine/docs/add-on/backup-for-gke/how-to/restore-order
#
# IMPORTANT YAML field names (NOT the non-standard variants):
#   - "satisfying" / "requiring" (NOT "satisfyingGroupKind" / "requiringGroupKinds")
#   - "resourceGroup" / "resourceKind" (NOT nested under groupKind)
#   - group="" for core resources (Pod, Secret, ConfigMap)
#
# NOTE: PVC->Workloads and ServiceAccount->Workloads are already default dependencies,
# so only Secret->Workloads and ConfigMap->Workloads need to be added.
# Workload kinds covered: Pod, Deployment, ReplicaSet, StatefulSet, DaemonSet, Job, CronJob.
RESTORE_ORDER_TEMPLATE = """groupKindDependencies:
  # Secret must be restored before all workload types
  - satisfying:
      resourceGroup: ""
      resourceKind: Secret
    requiring:
      resourceGroup: ""
      resourceKind: Pod
  - satisfying:
      resourceGroup: ""
      resourceKind: Secret
    requiring:
      resourceGroup: apps
      resourceKind: Deployment
  - satisfying:
      resourceGroup: ""
      resourceKind: Secret
    requiring:
      resourceGroup: apps
      resourceKind: ReplicaSet
  - satisfying:
      resourceGroup: ""
      resourceKind: Secret
    requiring:
      resourceGroup: apps
      resourceKind: StatefulSet
  - satisfying:
      resourceGroup: ""
      resourceKind: Secret
    requiring:
      resourceGroup: apps
      resourceKind: DaemonSet
  - satisfying:
      resourceGroup: ""
      resourceKind: Secret
    requiring:
      resourceGroup: batch
      resourceKind: Job
  - satisfying:
      resourceGroup: ""
      resourceKind: Secret
    requiring:
      resourceGroup: batch
      resourceKind: CronJob
  # ConfigMap must be restored before all workload types
  - satisfying:
      resourceGroup: ""
      resourceKind: ConfigMap
    requiring:
      resourceGroup: ""
      resourceKind: Pod
  - satisfying:
      resourceGroup: ""
      resourceKind: ConfigMap
    requiring:
      resourceGroup: apps
      resourceKind: Deployment
  - satisfying:
      resourceGroup: ""
      resourceKind: ConfigMap
    requiring:
      resourceGroup: apps
      resourceKind: ReplicaSet
  - satisfying:
      resourceGroup: ""
      resourceKind: ConfigMap
    requiring:
      resourceGroup: apps
      resourceKind: StatefulSet
  - satisfying:
      resourceGroup: ""
      resourceKind: ConfigMap
    requiring:
      resourceGroup: apps
      resourceKind: DaemonSet
  - satisfying:
      resourceGroup: ""
      resourceKind: ConfigMap
    requiring:
      resourceGroup: batch
      resourceKind: Job
  - satisfying:
      resourceGroup: ""
      resourceKind: ConfigMap
    requiring:
      resourceGroup: batch
      resourceKind: CronJob
"""


def build_exclusion_filter_template() -> str:
    """Build exclusion filter YAML for cross-project restore.

    Excludes:
    1. Deployments, ReplicaSets, Pods with app label in (admin, deepdoc, parser, ragflow)
       -- these use project-specific images that cannot be pulled in the destination project.
    2. Cluster-scoped CiliumIdentity and CiliumNode -- these are cluster-specific and
       conflict on restore even with USE_BACKUP_VERSION policy.
    3. Gateway API resources -- these are control-plane-managed ingress resources and
       should not be replayed from backup into an existing cluster.

    Produces YAML like:
    exclusionFilters:
    - groupKind:
        resourceGroup: apps
        resourceKind: Deployment
      labels:
        app: admin
    ... (repeats for app labels and kinds)
    - groupKind:
        resourceGroup: cilium.io
        resourceKind: CiliumIdentity
    - groupKind:
        resourceGroup: cilium.io
        resourceKind: CiliumNode
    """
    lines = ["exclusionFilters:"]

    # 1. App-label-based exclusions (Deployment, ReplicaSet, Pod per label value)
    for app_name in EXCLUDE_APP_LABELS:
        lines.append("- groupKind:")
        lines.append("    resourceGroup: apps")
        lines.append("    resourceKind: Deployment")
        lines.append("  labels:")
        lines.append(f"    app: {app_name}")

    for app_name in EXCLUDE_APP_LABELS:
        lines.append("- groupKind:")
        lines.append("    resourceGroup: apps")
        lines.append("    resourceKind: ReplicaSet")
        lines.append("  labels:")
        lines.append(f"    app: {app_name}")

    for app_name in EXCLUDE_APP_LABELS:
        lines.append("- groupKind:")
        lines.append("    resourceGroup: \"\"")
        lines.append("    resourceKind: Pod")
        lines.append("  labels:")
        lines.append(f"    app: {app_name}")

    # 2. Cilium cluster-scoped exclusions (no namespace, no labels needed)
    for kind in EXCLUDE_CILIUM_KINDS:
        lines.append("- groupKind:")
        lines.append("    resourceGroup: cilium.io")
        lines.append(f"    resourceKind: {kind}")

    # 3. Gateway API exclusions (both namespaced and cluster-scoped kinds)
    for kind in EXCLUDE_GATEWAY_API_KINDS:
        lines.append("- groupKind:")
        lines.append(f"    resourceGroup: {EXCLUDE_GATEWAY_API_GROUP}")
        lines.append(f"    resourceKind: {kind}")

    return "\n".join(lines)


EXCLUSION_FILTER_TEMPLATE = build_exclusion_filter_template()


# How to verify backup contents (e.g., check if Secrets are included):
#
# gcloud CLI:
#   gcloud beta container backup-restore backups get-backup-index-download-url BACKUP_NAME \
#     --project=PROJECT --location=LOCATION --backup-plan=PLAN_NAME
#   # Opens a signed URL to download the backup index JSON, then:
#   curl -s "SIGNED_URL" -o /tmp/backup-index.json
#   python3 -c "
#     import json
#     with open('/tmp/backup-index.json') as f:
#         data = json.load(f)
#     for ns, content in data['resources'].items():
#         print(ns)
# "
#
# Console:
#   Cloud Console -> Kubernetes Engine -> Backup for GKE -> Backups -> click PLAN ->
#   click Backup Name -> Backup index -> browse/search resources
#
# NOTE: Secrets (v1/Secret) are NOT included by default. The backup plan must be
# created with --include-secrets to include them. ConfigMaps are included by default.
def create_or_update_backup_plan(
    source_project: str,
    location: str,
    source_cluster: str,
    backup_plan_name: str,
    description: str
) -> Tuple[bool, str]:
    """Create or update a backup plan idempotently."""
    cluster_path = f"projects/{source_project}/locations/{location}/clusters/{source_cluster}"

    # Expected backup plan configuration
    expected_config = {
        "cluster": cluster_path,
        "backupConfig": {
            "allNamespaces": True,
            "includeSecrets": True,
            "includeVolumeData": True,
        },
        "backupSchedule": {
            "cronSchedule": "0 1 * * *",  # Daily at 1 AM
        },
        "retentionPolicy": {
            "backupRetainDays": 3,
        },
    }

    # Check if backup plan exists
    existing = get_backup_plan_by_name(source_project, location, backup_plan_name)

    if existing:
        if config_matches(expected_config, existing):
            print(f"  [OK] Backup plan '{backup_plan_name}' already exists with same configuration, skipping")
            return True, f"projects/{source_project}/locations/{location}/backupPlans/{backup_plan_name}"

        print("  Backup plan exists but differs, deleting backups first...")
        # List and delete all backups under this plan
        backups = list_backups(source_project, location, backup_plan_name)
        for backup in backups:
            backup_id = backup.get("name", "").rsplit("/", 1)[-1]
            print(f"    Deleting backup {backup_id}...")
            run_cmd(
                [
                    "gcloud", "beta", "container", "backup-restore", "backups", "delete",
                    backup_id,
                    "--project", source_project,
                    "--location", location,
                    "--backup-plan", backup_plan_name,
                    "--quiet"
                ],
                check=False
            )

        print("  Deleting backup plan...")
        run_cmd(
            [
                "gcloud", "beta", "container", "backup-restore", "backup-plans", "delete",
                backup_plan_name,
                "--project", source_project,
                "--location", location,
                "--quiet"
            ],
            check=False
        )

    print(f"\nCreating backup plan '{backup_plan_name}' in {source_project}...")

    cmd = [
        "gcloud", "beta", "container", "backup-restore", "backup-plans", "create",
        backup_plan_name,
        "--project", source_project,
        "--location", location,
        "--cluster", cluster_path,
        "--all-namespaces",
        "--include-secrets",
        "--include-volume-data",
        "--cron-schedule=0 1 * * *",  # Daily at 1 AM
        "--backup-retain-days=3",
    ]

    result = run_cmd(cmd, check=False)

    if result.returncode == 0:
        backup_plan_path = f"projects/{source_project}/locations/{location}/backupPlans/{backup_plan_name}"
        print(f"  [OK] Backup plan created: {backup_plan_path}")
        return True, backup_plan_path
    else:
        print(f"  [FAIL] Failed to create backup plan: {result.stderr}")
        return False, ""


def create_or_update_restore_plan(
    destination_project: str,
    location: str,
    destination_cluster: str,
    backup_plan: str,
    restore_plan_name: str,
    description: str
) -> Tuple[bool, str]:
    """Create or update a restore plan idempotently."""
    cluster_path = f"projects/{destination_project}/locations/{location}/clusters/{destination_cluster}"

    # Expected restore plan configuration
    # NOTE: restoreOrder is NOT part of the API's stored config (it is passed via
    # --restore-order-file at create time but not returned by describe), so we do
    # NOT include it in expected_config. The --restore-order-file is still passed
    # on every create to ensure the correct order is applied.
    expected_config = {
        "backupPlan": backup_plan,
        "cluster": cluster_path,
        "description": description,
        "restoreConfig": {
            "namespacedResourceRestoreMode": "MERGE_REPLACE_ON_CONFLICT",
            "volumeDataRestorePolicy": "RESTORE_VOLUME_DATA_FROM_BACKUP",
        },
        "clusterResourceRestoreScope": {
            "scope": "ALL_GROUP_KINDS",
            "conflictsPolicy": "USE_BACKUP_VERSION",
        },
    }

    # Check if restore plan exists
    existing = get_restore_plan_config(destination_project, location, restore_plan_name)

    if existing:
        if config_matches(expected_config, existing):
            print(f"  [OK] Restore plan '{restore_plan_name}' already exists with same configuration, skipping")
            return True, existing.get("name", "")

        # Delete and recreate
        print("  Restore plan exists but differs, deleting for recreation...")
        run_cmd(
            [
                "gcloud", "beta", "container", "backup-restore", "restore-plans", "delete",
                restore_plan_name,
                "--project", destination_project,
                "--location", location,
                "--force",
                "--quiet"
            ],
            check=False
        )

    print(f"\nCreating restore plan '{restore_plan_name}' in {destination_project}...")

    # Write restore order template to a temp file to enforce Secret before Workload
    import tempfile
    with tempfile.NamedTemporaryFile(mode='w', suffix='.yaml', delete=False) as f:
        f.write(RESTORE_ORDER_TEMPLATE)
        restore_order_file = f.name

    cmd = [
        "gcloud", "beta", "container", "backup-restore", "restore-plans", "create",
        restore_plan_name,
        "--project", destination_project,
        "--location", location,
        "--cluster", cluster_path,
        "--backup-plan", backup_plan,
        "--all-namespaces",
        "--cluster-resource-scope-all-group-kinds",
        "--cluster-resource-conflict-policy", "use-backup-version",
        "--namespaced-resource-restore-mode", "merge-replace-on-conflict",
        "--volume-data-restore-policy", "restore-volume-data-from-backup",
        "--description", description,
        "--restore-order-file", restore_order_file,
    ]

    result = run_cmd(cmd, check=False)

    import os
    os.unlink(restore_order_file)

    if result.returncode == 0:
        restore_plan_path = f"projects/{destination_project}/locations/{location}/restorePlans/{restore_plan_name}"
        print(f"  [OK] Restore plan created: {restore_plan_path}")
        return True, restore_plan_path
    else:
        if "already exists" in result.stderr.lower():
            print("  [OK] Restore plan already exists")
            return True, f"projects/{destination_project}/locations/{location}/restorePlans/{restore_plan_name}"
        print(f"  [FAIL] Failed to create restore plan: {result.stderr}")
        return False, ""


def select_backup(
    source_project: str,
    location: str,
    backup_plan_name: str
) -> Optional[str]:
    """List existing backups and prompt user to select one or create new.

    Returns:
        Backup name selected by user, or None to skip backup/restore
    """
    backups = list_backups(source_project, location, backup_plan_name)

    if not backups:
        return None  # No existing backups, will create new

    # Sort by createTime descending (newest first)
    backups.sort(key=lambda b: b.get("createTime", ""), reverse=True)

    print("\n  Existing backups:")
    for i, bk in enumerate(backups, 1):
        bk_name = bk.get("name", "").rsplit("/", 1)[-1]
        bk_state = bk.get("state", "UNKNOWN")
        bk_time = bk.get("createTime", "")[:19].replace("T", " ")
        print(f"    {i}. {bk_name} ({bk_state}, {bk_time})")

    try:
        choice = input("\n  Select backup (number) or 0 to create new: ").strip()
        if choice == "0":
            return None  # Create new backup
        idx = int(choice) - 1
        if 0 <= idx < len(backups):
            return backups[idx].get("name", "").rsplit("/", 1)[-1]
    except (ValueError, EOFError):
        pass

    return None


def trigger_backup(
    source_project: str,
    location: str,
    backup_plan_name: str,
    source_cluster: str
) -> Tuple[bool, str]:
    """Trigger a backup for the source cluster."""
    backup_name = f"backup-{source_cluster}-{int(time.time())}"

    print(f"\nTriggering backup '{backup_name}' on source cluster...")

    cmd = [
        "gcloud", "beta", "container", "backup-restore", "backups", "create",
        backup_name,
        "--project", source_project,
        "--location", location,
        "--backup-plan", backup_plan_name,
    ]

    result = run_cmd(cmd, check=False)

    if result.returncode == 0:
        print(f"  [OK] Backup triggered: {backup_name}")
        return True, backup_name
    else:
        print(f"  [FAIL] Failed to trigger backup: {result.stderr}")
        return False, ""


def wait_for_backup(
    source_project: str,
    location: str,
    backup_plan_name: str,
    backup_name: str,
    timeout_minutes: int = 30
) -> Tuple[bool, dict]:
    """Wait for a backup to complete."""
    print(f"  Waiting for backup '{backup_name}' to complete (timeout: {timeout_minutes}min)...")

    start_time = time.time()
    deadline = start_time + (timeout_minutes * 60)

    while time.time() < deadline:
        result = run_cmd(
            [
                "gcloud", "beta", "container", "backup-restore", "backups", "describe",
                backup_name,
                "--project", source_project,
                "--location", location,
                "--backup-plan", backup_plan_name,
                "--format", "json"
            ],
            check=False
        )

        if result.returncode == 0:
            try:
                backup = json.loads(result.stdout)
                state = backup.get("state", "")
                print(f"    Backup state: {state}")

                if "succeeded" in state.lower():
                    print("  [OK] Backup completed successfully")
                    return True, backup
                elif "failed" in state.lower() or "error" in state.lower():
                    print("  [FAIL] Backup failed")
                    return False, backup
            except json.JSONDecodeError:
                pass

        time.sleep(30)

    print(f"  [FAIL] Backup timed out after {timeout_minutes} minutes")
    return False, {}


def trigger_restore(
    destination_project: str,
    location: str,
    restore_plan_name: str,
    backup_name: str,
    source_project: str,
    backup_plan_name: str,
    filter_file_path: str = None
) -> Tuple[bool, str]:
    """Trigger a restore for the destination cluster."""
    restore_name = f"restore-{int(time.time())}"

    print(f"\nTriggering restore '{restore_name}' on destination cluster...")

    backup_path = f"projects/{source_project}/locations/{location}/backupPlans/{backup_plan_name}/backups/{backup_name}"

    cmd = [
        "gcloud", "alpha", "container", "backup-restore", "restores", "create",
        restore_name,
        "--project", destination_project,
        "--location", location,
        "--restore-plan", restore_plan_name,
        "--backup", backup_path,
    ]

    if filter_file_path:
        cmd.extend(["--filter-file", filter_file_path])

    result = run_cmd(cmd, check=False)

    if result.returncode == 0:
        print(f"  [OK] Restore triggered: {restore_name}")
        return True, restore_name
    else:
        print(f"  [FAIL] Failed to trigger restore: {result.stderr}")
        return False, ""


def wait_for_restore(
    destination_project: str,
    location: str,
    restore_plan_name: str,
    restore_name: str,
    timeout_minutes: int = 30
) -> Tuple[bool, dict]:
    """Wait for a restore to complete."""
    print(f"  Waiting for restore '{restore_name}' to complete (timeout: {timeout_minutes}min)...")

    start_time = time.time()
    deadline = start_time + (timeout_minutes * 60)

    while time.time() < deadline:
        result = run_cmd(
            [
                "gcloud", "beta", "container", "backup-restore", "restores", "describe",
                restore_name,
                "--project", destination_project,
                "--location", location,
                "--restore-plan", restore_plan_name,
                "--format", "json"
            ],
            check=False
        )

        if result.returncode == 0:
            try:
                restore = json.loads(result.stdout)
                state = restore.get("state", "")
                print(f"    Restore state: {state}")

                if "succeeded" in state.lower():
                    print("  [OK] Restore completed successfully")
                    return True, restore
                elif "failed" in state.lower() or "error" in state.lower():
                    print("  [FAIL] Restore failed")
                    return False, restore
            except json.JSONDecodeError:
                pass

        time.sleep(30)

    print(f"  [FAIL] Restore timed out after {timeout_minutes} minutes")
    return False, {}


def parse_source_dest(value: str) -> Tuple[str, str]:
    """Parse source/destination argument in format: project.cluster.

    Returns:
        Tuple of (project, cluster)
    """
    if "." not in value:
        raise argparse.ArgumentTypeError(
            f"Invalid format '{value}'. Expected 'project.cluster'"
        )
    parts = value.rsplit(".", 1)
    return parts[0], parts[1]


def fix_k8s_sa_annotation_after_cross_project_restore(project: str, gke_namespace: str = "ragflow") -> bool:
    """Fix K8s ServiceAccount annotation after cross-project restore.

    CRITICAL: This is a MUST action after restoring from GKE backup to a different project.
    GKE backup/restore preserves K8s ServiceAccount annotations but they may reference
    the old project's GCP service account, causing Workload Identity to fail.

    This function checks if the K8s default ServiceAccount annotation matches the
    expected GCP service account for the current project, and updates it if needed.

    Args:
        project: GCP project ID (e.g., "ragflow-stage")
        gke_namespace: Kubernetes namespace where the ServiceAccount is located

    Returns:
        True if annotation is correct or was successfully updated, False otherwise

    Example:
        # After restoring from ragflow-488401 to ragflow-stage:
        fix_k8s_sa_annotation_after_cross_project_restore("ragflow-stage", "ragflow")
    """
    print("\n" + "=" * 70)
    print("Fixing K8s ServiceAccount Annotation After Cross-Project Restore")
    print("=" * 70)

    # Get expected GCP service account for this project
    expected_gcp_sa = f"ragflow-gcs@{project}.iam.gserviceaccount.com"

    # Get current annotation from K8s ServiceAccount
    result = run_cmd(
        f"kubectl get serviceaccount default -n {gke_namespace} "
        f"-o jsonpath='{{.metadata.annotations.iam.gke.io/gcp-service-account}}'",
        check=False,
    )
    if result.returncode != 0:
        print(f"  Error: Could not get K8s ServiceAccount: {result.stderr}")
        return False

    current_annotation = result.stdout.strip()

    if not current_annotation:
        print("  Warning: K8s ServiceAccount has no iam.gke.io/gcp-service-account annotation")
        print("  This may cause Workload Identity to not work.")
        print(f"  Run: kubectl annotate serviceaccount default -n {gke_namespace} "
              f"iam.gke.io/gcp-service-account={expected_gcp_sa} --overwrite")
        return False

    if current_annotation == expected_gcp_sa:
        print(f"  K8s SA annotation is correct: {current_annotation}")
        return True

    print("  WARNING: K8s SA annotation mismatch detected!")
    print(f"    Current:   {current_annotation}")
    print(f"    Expected:   {expected_gcp_sa}")
    print("  This will cause Workload Identity to fail with 403 Forbidden errors.")

    # Update the annotation
    print(f"\n  Updating annotation to {expected_gcp_sa}...")
    result = run_cmd(
        f"kubectl annotate serviceaccount default -n {gke_namespace} "
        f"iam.gke.io/gcp-service-account={expected_gcp_sa} --overwrite",
        check=False,
    )
    if result.returncode == 0:
        print("  SUCCESS: K8s SA annotation updated")
        print("  Note: Restart pods to apply the new Workload Identity mapping")
        return True
    else:
        print(f"  Error: Could not update annotation: {result.stderr}")
        return False


def cmd_plan(source_project: str, source_cluster: str, dest_project: str, dest_cluster: str):
    """List backup plans, backups, and restore plans with console links."""
    print("=" * 70)
    print("GKE Backup/Restore Plan Status")
    print("=" * 70)
    print(f"\nSource:      {source_project}/{source_cluster}")
    print(f"Destination: {dest_project}/{dest_cluster}")

    is_cross_project = source_project != dest_project

    # Pre-flight check
    print("\n" + "-" * 70)
    print("Pre-flight Check")
    print("-" * 70)
    projects_to_check = [source_project]
    if is_cross_project:
        projects_to_check.append(dest_project)
    if not check_prerequisites(projects_to_check):
        print("\n[FAIL] Prerequisites check failed. Please fix the issues above and try again.")
        sys.exit(1)
    print("  [OK] Prerequisites check passed")

    # Get cluster location
    cluster_info = get_cluster_info(source_project, source_cluster)
    if not cluster_info:
        print(f"\n[FAIL] Cluster {source_cluster} not found in project {source_project}")
        sys.exit(1)
    location, _ = cluster_info

    # Default plan names
    backup_plan_name = f"backup-plan-{source_cluster}"
    restore_plan_name = f"restore-plan-{source_cluster}-to-{dest_cluster}"

    # Source backup plans
    print("\n" + "-" * 70)
    print("SOURCE BACKUP PLANS")
    print("-" * 70)
    print(f"Console: {get_console_url('backupPlans', source_project)}")
    backup_plans = list_backup_plans(source_project)
    for bp in backup_plans:
        name = bp.get("name", "")
        short_name = name.rsplit("/", 1)[-1] if "/" in name else name
        state = bp.get("state", "UNKNOWN")
        last_backup = bp.get("lastSuccessfulBackupTime", "Never")

        is_our_plan = short_name == backup_plan_name
        marker = " [DEFAULT]" if is_our_plan else ""
        print(f"  {short_name}{marker}: {state}, last backup: {last_backup}")

        # List backups for this plan
        if location:
            backups = list_backups(source_project, location, short_name)
            if backups:
                for bk in backups[-3:]:  # Show last 3 backups
                    bk_name = bk.get("name", "").rsplit("/", 1)[-1]
                    bk_state = bk.get("state", "UNKNOWN")
                    bk_time = bk.get("createTime", "")
                    print(f"    - {bk_name}: {bk_state} ({bk_time})")
                if len(backups) > 3:
                    print(f"    ... and {len(backups) - 3} more")

    # Destination restore plans
    print("\n" + "-" * 70)
    print("DESTINATION RESTORE PLANS")
    print("-" * 70)
    print(f"Console: {get_console_url('restorePlans', dest_project)}")
    restore_plans = list_restore_plans(dest_project)
    for rp in restore_plans:
        name = rp.get("name", "")
        short_name = name.rsplit("/", 1)[-1] if "/" in name else name
        state = rp.get("state", "UNKNOWN")

        is_our_plan = short_name == restore_plan_name
        marker = " [DEFAULT]" if is_our_plan else ""
        print(f"  {short_name}{marker}: {state}")

    if not restore_plans:
        print("  (none)")

    # Cross-project notice
    if is_cross_project:
        print("\n" + "-" * 70)
        print("CROSS-PROJECT NOTICE")
        print("-" * 70)
        print("  For cross-project restore, these resources will be EXCLUDED")
        print("  (they use project-specific images that cannot be pulled in destination):")
        print("  Deployments/ReplicaSets/Pods (by app label):")
        for app in EXCLUDE_APP_LABELS:
            print(f"    - app={app}")
        print("  (Deployment, ReplicaSet, Pod each with resourceGroup apps/apps/\"\" respectively)")

    print("\n" + "=" * 70)


def cmd_apply(source_project: str, source_cluster: str, dest_project: str, dest_cluster: str):
    """Execute backup and restore workflow."""
    print("=" * 70)
    print("GKE Cross-Project Backup Restore Automation")
    print("=" * 70)
    print(f"\nSource:      {source_project}/{source_cluster}")
    print(f"Destination: {dest_project}/{dest_cluster}")

    is_cross_project = source_project != dest_project
    print(f"\nMode:        {'Cross-project' if is_cross_project else 'Same-project'} restore")

    # Default plan names
    backup_plan_name = f"backup-plan-{source_cluster}"
    restore_plan_name = f"restore-plan-{source_cluster}-to-{dest_cluster}"
    backup_description = f"Backup plan for {source_cluster}"
    restore_description = f"Restore plan from {source_cluster} to {dest_cluster}"

    # Pre-flight check: verify gcloud CLI and authentication
    print("\n" + "=" * 70)
    print("Pre-flight Check")
    print("=" * 70)

    projects_to_check = [source_project]
    if is_cross_project:
        projects_to_check.append(dest_project)

    if not check_prerequisites(projects_to_check):
        print("\n[FAIL] Prerequisites check failed. Please fix the issues above and try again.")
        print("\nTip: Run 'gcloud auth login' to authenticate, then ensure your account has:")
        print("  - Source Project: roles/gkebackup.backupAdmin")
        if is_cross_project:
            print("  - Destination Project: roles/gkebackup.restoreAdmin, roles/gkebackup.backupAdmin")
        sys.exit(1)
    print("  [OK] Prerequisites check passed")

    # Step 1: Enable APIs
    print("\n" + "=" * 70)
    print("Step 1: Enabling APIs")
    print("=" * 70)

    enable_api(source_project)
    if is_cross_project:
        enable_api(dest_project)

    # Step 2: Get cluster location
    print("\n" + "=" * 70)
    print("Step 2: Getting Cluster Information")
    print("=" * 70)

    cluster_info = get_cluster_info(source_project, source_cluster)
    if not cluster_info:
        print(f"\n[FAIL] Cluster {source_cluster} not found in project {source_project}")
        sys.exit(1)
    location, _ = cluster_info
    print(f"  Location: {location}")

    # Step 3: Verify destination cluster exists
    print("\n" + "=" * 70)
    print("Step 3: Verifying Destination Cluster")
    print("=" * 70)

    clusters = list_clusters(dest_project)
    cluster_exists = any(dest_cluster in c.get("name", "") for c in clusters)

    if not cluster_exists:
        print(f"  [FAIL] Cluster '{dest_cluster}' not found in {dest_project}")
        print("  Available clusters:")
        for c in clusters:
            print(f"    - {c.get('name')} ({c.get('location')})")
        sys.exit(1)
    print(f"  [OK] Cluster '{dest_cluster}' found")

    # Step 4: Configure cross-project access
    if is_cross_project:
        print("\n" + "=" * 70)
        print("Step 4: Configuring Cross-Project Access")
        print("=" * 70)

        # Create service account in destination project
        sa_email = create_service_account(dest_project)

        # Grant restoreAdmin in destination project
        grant_iam_role(
            dest_project,
            sa_email,
            "roles/gkebackup.restoreAdmin"
        )

        # Grant backupAdmin in source project
        grant_iam_role(
            source_project,
            sa_email,
            "roles/gkebackup.backupAdmin"
        )

    # Step 5: Create or update backup plan (idempotent)
    print("\n" + "=" * 70)
    print("Step 5: Creating Backup Plan (idempotent)")
    print("=" * 70)

    backup_plan_full = f"projects/{source_project}/locations/{location}/backupPlans/{backup_plan_name}"

    bp_success, bp_path = create_or_update_backup_plan(
        source_project,
        location,
        source_cluster,
        backup_plan_name,
        backup_description
    )

    if not bp_success:
        print("\n[FAIL] Failed to create backup plan")
        sys.exit(1)
    backup_plan_full = bp_path

    # Step 6: Create restore channel (cross-project only; same-project has no need for it)
    if is_cross_project:
        print("\n" + "=" * 70)
        print("Step 6: Creating Restore Channel")
        print("=" * 70)

        channel_name = f"restore-to-{dest_project}"
        channel_success, channel_path = create_restore_channel(
            source_project,
            location,
            dest_project,
            channel_name
        )

        if not channel_success:
            print("\n[FAIL] Failed to create restore channel")
            sys.exit(1)

    # Step 7: Create or update restore plan (idempotent)
    print("\n" + "=" * 70)
    print("Step 7: Creating Restore Plan (idempotent)")
    print("=" * 70)

    plan_success, plan_path = create_or_update_restore_plan(
        dest_project,
        location,
        dest_cluster,
        backup_plan_full,
        restore_plan_name,
        restore_description
    )

    if not plan_success:
        print("\n[FAIL] Failed to create restore plan")
        sys.exit(1)

    # Get and display restore plan config
    config = get_restore_plan_config(dest_project, location, restore_plan_name)
    if config:
        print("\nRestore Plan Configuration:")
        print(f"  - Name: {config.get('name')}")
        print(f"  - State: {config.get('state')}")
        print(f"  - Backup Plan: {config.get('backupPlan')}")
        print(f"  - Cluster: {config.get('cluster')}")

    # Step 8: Select or create backup on source cluster
    print("\n" + "=" * 70)
    print("Step 8: Selecting Backup on Source Cluster")
    print("=" * 70)

    selected_backup = select_backup(source_project, location, backup_plan_name)

    if selected_backup:
        print(f"\n  Using existing backup: {selected_backup}")
        backup_name = selected_backup
        # Skip Step 9 (waiting for backup) since backup already exists
    else:
        # Create new backup
        print("\n  Creating new backup...")
        bp_success, backup_name = trigger_backup(
            source_project,
            location,
            backup_plan_name,
            source_cluster
        )

        if not bp_success:
            print("\n[FAIL] Failed to trigger backup")
            sys.exit(1)

        # Step 9: Wait for backup to complete
        print("\n" + "=" * 70)
        print("Step 9: Waiting for Backup to Complete")
        print("=" * 70)

        bp_success, backup_result = wait_for_backup(
            source_project,
            location,
            backup_plan_name,
            backup_name
        )

        if not bp_success:
            print("\n[FAIL] Backup failed or timed out")
            sys.exit(1)

    # Extract backup name from the full path for restore
    if "/" in backup_name:
        backup_name = backup_name.rsplit("/", 1)[-1]

    # Step 10: Trigger restore on destination cluster
    print("\n" + "=" * 70)
    print("Step 10: Triggering Restore on Destination Cluster")
    print("=" * 70)

    # Create exclusion filter file for all restores.
    # - Cross-project: excludes app-labeled workloads (project-specific images) and
    #   Cilium identities (cluster-specific, not migratable).
    # - Same-project: excludes CiliumIdentity/CiliumNode which already exist on cluster
    #   and would conflict on restore.
    import tempfile
    with tempfile.NamedTemporaryFile(mode='w', suffix='.yaml', delete=False) as f:
        f.write(EXCLUSION_FILTER_TEMPLATE)
        filter_file_path = f.name
    print(f"  Exclusion filter: {filter_file_path}")

    try:
        restore_success, restore_name = trigger_restore(
            dest_project,
            location,
            restore_plan_name,
            backup_name,
            source_project,
            backup_plan_name,
            filter_file_path
        )

        if not restore_success:
            print("\n[FAIL] Failed to trigger restore")
            sys.exit(1)
    finally:
        if filter_file_path:
            import os
            os.unlink(filter_file_path)

    # Step 11: Wait for restore to complete
    print("\n" + "=" * 70)
    print("Step 11: Waiting for Restore to Complete")
    print("=" * 70)

    restore_success, restore_result = wait_for_restore(
        dest_project,
        location,
        restore_plan_name,
        restore_name
    )

    if not restore_success:
        print("\n[FAIL] Restore failed or timed out")
        sys.exit(1)


    # Step 12: Fix K8s SA annotation after cross-project restore
    if is_cross_project:
        fix_k8s_sa_annotation_after_cross_project_restore(dest_project, "ragflow")

    print("\n" + "=" * 70)
    print("[OK] Backup and Restore Complete!")
    print("=" * 70)


def main():
    parser = argparse.ArgumentParser(
        description="GKE Backup and Restore - Manage backup/restore plans and execute backup/restore",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Subcommands:
  plan   List backup plans, backups, and restore plans with console links
  apply  Create backup plan, trigger backup, create restore plan, execute restore

Examples:
  # List current backup/restore status
  python3 gke_backup_restore.py plan prod-project.prod-cluster-1 stage-project.stage-cluster-1

  # Execute backup and restore
  python3 gke_backup_restore.py apply prod-project.prod-cluster-1 stage-project.stage-cluster-1
        """
    )

    parser.add_argument(
        "command",
        choices=["plan", "apply"],
        help="Subcommand: plan (list status) or apply (execute backup/restore)"
    )
    parser.add_argument(
        "source",
        type=parse_source_dest,
        help="Source GKE in format: project.cluster"
    )
    parser.add_argument(
        "destination",
        type=parse_source_dest,
        help="Destination GKE in format: project.cluster"
    )

    args = parser.parse_args()

    source_project, source_cluster = args.source
    dest_project, dest_cluster = args.destination

    if args.command == "plan":
        cmd_plan(source_project, source_cluster, dest_project, dest_cluster)
    elif args.command == "apply":
        cmd_apply(source_project, source_cluster, dest_project, dest_cluster)


if __name__ == "__main__":
    main()
