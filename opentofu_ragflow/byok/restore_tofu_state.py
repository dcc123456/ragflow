#!/usr/bin/env python3
"""
Incremental import: discover resources that exist in tofu plan AND K8s cluster
BUT are missing from state file. No state deletion needed.

Logic:
  1. Parse current tofu state file → existing resources
  2. tofu plan -json → tofu resource addresses + evaluated metadata.name (single source)
  3. kubectl discover cluster resources → live resources
  4. Import: resources in (plan ∩ K8s) - state

We use 'tofu plan -json' because it provides both the resource address
AND the evaluated metadata.name (resolves variables/interpolations correctly),
which is essential for mapping tofu resources to actual K8s resources.
"""

import base64
import json
import subprocess
import re
import os


def run_cmd(cmd, timeout=30):
    result = subprocess.run(cmd, shell=True, capture_output=True, text=True, timeout=timeout)
    return result.stdout.strip(), result.stderr.strip(), result.returncode


def get_planned_resources():
    """
    Generate a plan and parse its JSON output to extract:
    - All kubernetes resource addresses planned (create/update/no-op)
    - Evaluated metadata.name for each resource
    - For kubernetes_manifest: apiVersion, kind, name, namespace
    - Plan summary: create, replace, destroy actions
    - Random password dependencies (which k8s secrets depend on which random_password)
    - K8s secret current data (before state) for extracting passwords

    Returns:
        (planned_resources, metadata_names, plan_summary, manifest_info, plan_data)
        - planned_resources: set of resource addresses from plan
        - metadata_names: dict mapping address -> k8s_metadata_name
        - plan_summary: dict with lists for 'create', 'replace', 'destroy', 'update'
        - manifest_info: dict mapping address -> {api_version, kind, name, namespace} for manifests
        - plan_data: full plan JSON data for further processing
    """
    # 1. Generate plan to a temporary file
    _, _, rc = run_cmd("tofu plan -out=import_discovery.tfplan", timeout=120)
    if rc != 0:
        return set(), {}, {}, {}, {}

    # 2. Export plan as JSON
    stdout, _, rc = run_cmd("tofu show -json import_discovery.tfplan", timeout=120)

    # Clean up the temp plan file
    if os.path.exists("import_discovery.tfplan"):
        os.remove("import_discovery.tfplan")

    if rc != 0:
        return set(), {}, {}, {}, {}

    data = json.loads(stdout)
    planned = set()
    metadata_names = {}
    manifest_info = {}  # NEW: store full manifest metadata
    plan_summary = {"create": [], "replace": [], "destroy": [], "update": []}

    # 3. Parse resource changes
    for change in data.get("resource_changes", []):
        address = change["address"]
        if not address.startswith("kubernetes_"):
            continue

        planned.add(address)

        # Normalize address for metadata lookup keys (strip index to match state file format)
        # e.g., "kubernetes_job_v1.foo[0]" -> "kubernetes_job_v1.foo"
        addr_base = address.split("[")[0]

        actions = change.get("change", {}).get("actions", [])

        # Classify action
        if actions == ["delete"]:
            plan_summary["destroy"].append(address)
        elif "delete" in actions and "create" in actions:
            plan_summary["replace"].append(address)
        elif "create" in actions:
            plan_summary["create"].append(address)
        elif "update" in actions:
            plan_summary["update"].append(address)

        # Only extract metadata.name for resources with pending changes
        if "create" not in actions:
            continue

        after = change.get("change", {}).get("after", {})
        if not after:
            continue

        # Extract name for standard kubernetes_* resources
        if "metadata" in after and isinstance(after["metadata"], list) and len(after["metadata"]) > 0:
            k8s_name = after["metadata"][0].get("name")
            if k8s_name:
                metadata_names[addr_base] = k8s_name

        # Extract info for kubernetes_manifest (nested manifest dict)
        elif "manifest" in after and isinstance(after["manifest"], dict):
            manifest = after["manifest"]
            k8s_name = manifest.get("metadata", {}).get("name")
            if k8s_name:
                metadata_names[addr_base] = k8s_name
            # Store full manifest info for dynamic import ID generation
            manifest_info[addr_base] = {
                "api_version": manifest.get("apiVersion"),
                "kind": manifest.get("kind"),
                "name": k8s_name,
                "namespace": manifest.get("metadata", {}).get("namespace", "default")
            }

    return planned, metadata_names, plan_summary, manifest_info, data


def parse_state_file(path):
    """Parse tofu state file, return set of resource addresses"""
    if not os.path.exists(path):
        return set()
    # Delete empty state file
    if os.path.getsize(path) == 0:
        os.remove(path)
        return set()
    with open(path) as f:
        state = json.load(f)
    return {r["type"] + "." + r["name"] for r in state.get("resources", [])}


def kubectl_get(resource_type):
    """
    Query K8s API, return dict of resource name -> full_id (namespace/name).
    Queries across all namespaces.
    """
    cmd = f"kubectl get {resource_type} -A -o json"
    stdout, _, rc = run_cmd(cmd)
    if rc != 0:
        return {}
    data = json.loads(stdout)
    result = {}
    for item in data.get("items", []):
        name = item["metadata"]["name"]
        ns = item["metadata"].get("namespace", "default")
        result[name] = f"{ns}/{name}"
    return result


def discover_cluster_resources():
    """Discover all relevant K8s resources across all namespaces"""
    resources = {}
    # Cluster-scoped
    resources["namespace"] = kubectl_get("namespace")
    # Namespaced resources
    for kind in ["pvc", "statefulset", "secret", "configmap", "service",
                 "deployment", "serviceaccount", "role", "rolebinding", "job", "cronjob"]:
        resources[kind] = kubectl_get(kind)
    # Gateway API (cluster-scoped but queried with -A)
    resources["gateway"] = kubectl_get("gateway")
    resources["httproute"] = kubectl_get("httproute")
    # Custom GKE resources
    resources["computeclass"] = kubectl_get("computeclass")
    resources["gcpbackendpolicy"] = kubectl_get("gcpbackendpolicy")
    return resources


def address_to_k8s_id(address, cluster, metadata_names=None, manifest_info=None):
    """
    Map tofu resource address → K8s resource ID(s).
    Generic implementation using pre-fetched cluster data and dynamic type inference.

    Returns:
        - None: no matching K8s resource found
        - str: exactly one match found (exact name match)
        - list of (k8s_id, match_type) tuples: multiple candidates, user should choose
          match_type is "exact" or "fuzzy"
    """
    metadata_names = metadata_names or {}
    manifest_info = manifest_info or {}
    if "." not in address:
        return None

    tf_type, tf_name = address.split(".", 1)
    tf_name = tf_name.split("[")[0]  # Remove index if present

    # Get the evaluated K8s resource name from plan JSON
    # metadata_names is keyed by addr_base (without index)
    addr_base = address.split("[")[0]
    k8s_name = metadata_names.get(addr_base, tf_name)

    def search_cluster(k8s_kind):
        """Search pre-fetched cluster dict. Zero subprocess calls."""
        if k8s_kind not in cluster:
            return []
        exact, fuzzy = [], []
        for name_key, full_id in cluster[k8s_kind].items():
            if name_key == k8s_name:
                exact.append((full_id, "exact"))
            elif k8s_name in name_key or name_key in k8s_name:
                fuzzy.append((full_id, "fuzzy"))

        # If we have exactly one exact match, prioritize it to avoid false ambiguity
        if len(exact) == 1:
            return exact
        return exact + fuzzy

    # --- Case 0: helm_release resources ---
    if tf_type == "helm_release":
        # helm_release import format: "release_name/namespace"
        # e.g., helm_release.eck_operator -> eck_operator/elastic-system
        # We need to find the namespace from plan data
        ns = metadata_names.get(address + "_namespace", "default")
        return f"{k8s_name}/{ns}"

    # --- Case 1: Kubernetes Manifests (Dynamic based on manifest info from plan) ---
    if tf_type == "kubernetes_manifest":
        info = manifest_info.get(addr_base)
        if not info:
            # Fallback: try to infer from tf_name
            return None

        api_version = info.get("api_version", "")
        kind = info.get("kind", "")
        namespace = info.get("namespace", "default")
        name = info.get("name", k8s_name)

        # Determine k8s kind for cluster lookup (lowercase, dashed)
        kind_lower = kind.lower().replace(" ", "")  # e.g., "ServiceAccount" -> "serviceaccount"

        matches = search_cluster(kind_lower)
        if not matches:
            return None

        # kubernetes_manifest import requires full apiVersion,kind,namespace,name format
        import_id = f"apiVersion={api_version},kind={kind},namespace={namespace},name={name}"

        if len(matches) == 1:
            return import_id
        return [(import_id, match_type) for _, match_type in matches]

    # --- Case 2: Standard K8s Resources (Generic mapping) ---
    # Dynamically extract K8s kind (e.g., "kubernetes_config_map_v1" -> "configmap")
    kind_match = re.match(r"kubernetes_(.+?)(?:_v\d+)?$", tf_type)
    if not kind_match:
        return None

    k8s_kind = kind_match.group(1).replace("_", "")
    if k8s_kind == "persistentvolumeclaim":
        k8s_kind = "pvc"  # Align with the keys used in discover_cluster_resources()

    matches = search_cluster(k8s_kind)
    if not matches:
        return None

    # Format output (Namespaces only need 'name', others need 'namespace/name')
    def format_standard(full_id):
        return full_id.split("/")[-1] if k8s_kind == "namespace" else full_id

    if len(matches) == 1:
        return format_standard(matches[0][0])
    return [(format_standard(id), match_type) for id, match_type in matches]


def get_helm_releases():
    """
    Get helm release resources from tofu plan with their namespace.
    Returns dict mapping address -> (release_name, namespace)
    """
    _, _, rc = run_cmd("tofu plan -out=helm_discovery.tfplan", timeout=120)
    if rc != 0:
        return {}

    stdout, _, rc = run_cmd("tofu show -json helm_discovery.tfplan", timeout=120)
    if os.path.exists("helm_discovery.tfplan"):
        os.remove("helm_discovery.tfplan")

    if rc != 0:
        return {}

    data = json.loads(stdout)
    releases = {}

    for change in data.get("resource_changes", []):
        address = change["address"]
        if not address.startswith("helm_release."):
            continue

        after = change.get("change", {}).get("after", {})
        if not after:
            continue

        name = after.get("name")
        namespace = after.get("namespace", "default")
        if name:
            releases[address] = (name, namespace)

    return releases


def find_random_password_dependencies(plan_data):
    """
    Parse plan JSON configuration to find which k8s resources depend on random_password.
    Returns dict mapping random_password_addr -> {k8s_secret_addr, password_field}

    Since the plan JSON doesn't expose nested key mappings, we use heuristic matching:
    - For kubernetes_secret_v1.mysql (single random_password dep): data.password
    - For kubernetes_secret_v1.ragflow_env (multiple deps): match key names to rp names

    resource "kubernetes_secret_v1" "mysql" {
        data = {
            password = random_password.mysql.result
        }
    }

    resource "kubernetes_secret_v1" "ragflow_env" {
        data = {
            REDIS_PASSWORD = random_password.redis.result
            RABBITMQ_DEFAULT_PASS = random_password.rabbitmq.result
        }
    }
    """
    dependencies = {}
    config = plan_data.get("configuration", {})
    root = config.get("root_module", {})

    # First pass: collect all random_password addresses and their names
    random_passwords = {}  # addr -> name (e.g., "random_password.mysql" -> "mysql")
    for res in root.get("resources", []):
        addr = res.get("address", "")
        if addr.startswith("random_password."):
            name = addr.split(".")[-1]
            random_passwords[addr] = name

    # Second pass: for k8s secrets, find which random_passwords they depend on
    k8s_secret_deps = {}  # k8s_secret_addr -> [random_password_addrs]
    for res in root.get("resources", []):
        addr = res.get("address", "")
        if not addr.startswith("kubernetes_secret_v1"):
            continue

        expressions = res.get("expressions", {})
        data_expr = expressions.get("data", {})
        refs = data_expr.get("references", []) if isinstance(data_expr, dict) else []

        # Extract unique random_password addresses (format: "random_password.xxx.result" or "random_password.xxx")
        seen = set()
        rp_refs = []
        for r in refs:
            if r.startswith("random_password."):
                rp_addr = r.split(".result")[0]
                if rp_addr not in seen:
                    seen.add(rp_addr)
                    rp_refs.append(rp_addr)
        if rp_refs:
            k8s_secret_deps[addr] = rp_refs

    # Third pass: determine key mapping using kubectl data
    # (before.data is unreliable when resource not in state)
    k8s_passwords = get_k8s_secret_passwords(plan_data)

    for k8s_addr, rp_refs in k8s_secret_deps.items():
        # Get data keys from kubectl
        k8s_data_keys = set()
        for key in [k8s_addr, re.sub(r'\[\d+\]$', '', k8s_addr)]:
            if key in k8s_passwords:
                k8s_data_keys = set(k8s_passwords[key].keys())
                break

        if len(rp_refs) == 1:
            # Single dependency - use the first/only data key
            if k8s_data_keys:
                data_key = list(k8s_data_keys)[0]
                dependencies[rp_refs[0]] = {
                    "k8s_secret_addr": k8s_addr,
                    "password_field": data_key
                }
        else:
            # Multiple dependencies - use regex to match rp name to data keys
            rp_name_pattern = re.compile(r"random_password\.(\w+)")

            for rp_addr in rp_refs:
                # Skip if already matched (single dep case handled first)
                if rp_addr in dependencies:
                    continue

                match = rp_name_pattern.match(rp_addr)
                if not match:
                    continue
                rp_name = match.group(1)

                key_pattern = re.compile(
                    rf"^(.*_)?{re.escape(rp_name.upper())}(_.*)?_(PASSWORD|PASS|SECRET|KEY)$",
                    re.IGNORECASE
                )

                for data_key in k8s_data_keys:
                    if key_pattern.match(data_key):
                        dependencies[rp_addr] = {
                            "k8s_secret_addr": k8s_addr,
                            "password_field": data_key
                        }
                        break

    return dependencies


def get_k8s_secret_passwords(plan_data):
    """
    Get current password values from k8s secrets using kubectl.
    Always fetches from cluster since plan's before.data is unreliable.

    Returns dict mapping k8s_secret_addr -> {password_key: password_value}
    """
    passwords = {}
    config = plan_data.get("configuration", {})
    root = config.get("root_module", {})

    # Build addr -> (secret_name, namespace) mapping from configuration
    addr_to_secret = {}  # addr -> (secret_name, namespace)

    for res in root.get("resources", []):
        addr = res.get("address", "")
        if not addr.startswith("kubernetes_secret_v1"):
            continue

        metadata = res.get("expressions", {}).get("metadata", [{}])
        if not metadata or len(metadata) == 0:
            continue

        name_expr = metadata[0].get("name", {})
        secret_name = None
        if isinstance(name_expr, dict):
            secret_name = name_expr.get("constant_value")

        namespace = "ragflow"
        ns_expr = metadata[0].get("namespace", {})
        if isinstance(ns_expr, dict):
            ns_const = ns_expr.get("constant_value")
            if ns_const:
                namespace = ns_const

        if secret_name:
            addr_to_secret[addr] = (secret_name, namespace)
            # Also store base addr (without index) for indexed resources
            addr_base = re.sub(r'\[\d+\]$', '', addr)
            if addr_base != addr:
                addr_to_secret[addr_base] = (secret_name, namespace)

    # Fetch all secrets from cluster using kubectl once
    for addr, (secret_name, namespace) in addr_to_secret.items():
        kubectl_cmd = f"kubectl get secret {secret_name} -n {namespace} -o json"
        stdout, _, rc = run_cmd(kubectl_cmd)
        if rc == 0:
            secret_data = json.loads(stdout).get("data", {})
            if secret_data:
                # K8s secret data values are base64-encoded; decode them for random_password.result
                decoded = {}
                for k, v in secret_data.items():
                    try:
                        decoded[k] = base64.b64decode(v).decode('utf-8')
                    except Exception as exc:
                        raise ValueError(f"Failed to base64-decode secret '{addr}' key '{k}', value: {v!r}") from exc
                passwords[addr] = decoded

    return passwords


def get_random_password_attrs(plan_data):
    """
    Extract random_password resource attributes from plan configuration.
    Returns dict mapping random_password_addr -> {length, special, id, result, ...}
    """
    attrs = {}
    config = plan_data.get("configuration", {})
    root = config.get("root_module", {})

    for res in root.get("resources", []):
        addr = res.get("address", "")
        if not addr.startswith("random_password."):
            continue

        expressions = res.get("expressions", {})
        attrs[addr] = {}
        for attr_name, expr in expressions.items():
            if isinstance(expr, dict):
                if "constant_value" in expr:
                    attrs[addr][attr_name] = expr["constant_value"]
                elif "references" in expr:
                    # Skip computed references
                    pass

    return attrs


def restore_random_password_state(state_file, plan_data, state_resources):
    """
    After importing k8s resources, check if any random_password resources are missing.
    If they are, construct and write them to state by extracting passwords from
    the k8s secrets that depend on them.
    """
    # Find which k8s secrets depend on which random_password
    rp_dependencies = find_random_password_dependencies(plan_data)
    if not rp_dependencies:
        print("  No random_password dependencies found")
        return

    # Get current passwords from k8s secrets (before state)
    k8s_passwords = get_k8s_secret_passwords(plan_data)

    # Get random_password resource attributes from configuration
    rp_attrs = get_random_password_attrs(plan_data)

    # Find missing random_password resources
    missing_rps = {}
    for rp_addr in rp_dependencies:
        if rp_addr not in state_resources:
            dep_info = rp_dependencies[rp_addr]
            k8s_addr = dep_info["k8s_secret_addr"]
            password_key = dep_info["password_field"]

            if k8s_addr in k8s_passwords:
                password = k8s_passwords[k8s_addr].get(password_key)
                if password:
                    missing_rps[rp_addr] = {
                        "password": password,
                        "attrs": rp_attrs.get(rp_addr, {})
                    }

    if not missing_rps:
        print("  All random_password resources already in state")
        return

    print(f"\n=== Restoring {len(missing_rps)} random_password resources ===")

    # Read current state
    with open(state_file) as f:
        state = json.load(f)

    # Hardcoded template for random_password resource
    RANDOM_PASSWORD_TEMPLATE = {
        "mode": "managed",
        "type": "random_password",
        "name": "__NAME__",  # Placeholder, will be replaced
        "provider": "provider[\"registry.opentofu.org/hashicorp/random\"]",
        "instances": [{
            "schema_version": 3,
            "attributes": {
                "bcrypt_hash": None,
                "id": "none",
                "keepers": None,
                "length": 16,
                "lower": True,
                "min_lower": 0,
                "min_numeric": 0,
                "min_special": 0,
                "min_upper": 0,
                "number": True,
                "numeric": True,
                "override_special": None,
                "result": "__RESULT__",  # Placeholder, will be replaced
                "special": False,
                "upper": True,
            },
            "sensitive_attributes": [
                [{"type": "get_attr", "value": "bcrypt_hash"}],
                [{"type": "get_attr", "value": "result"}]
            ],
        }]
    }

    for rp_addr, info in missing_rps.items():
        password = info["password"]

        # Parse type and name
        _, rp_name = rp_addr.split(".", 1)

        # Create from template and modify
        rp_state = json.loads(json.dumps(RANDOM_PASSWORD_TEMPLATE))
        rp_state["name"] = rp_name
        rp_state["instances"][0]["attributes"]["result"] = password

        state["resources"].append(rp_state)
        print(f"  [ADD] {rp_addr} (result: {password[:10]}...)")

    # Write updated state
    with open(state_file, "w") as f:
        json.dump(state, f, indent=2)

    print(f"  State updated with {len(missing_rps)} random_password resources")


def main():
    # OpenTofu requires .tf files to be in the current directory (no upward search)
    tf_files = [f for f in os.listdir(".") if f.endswith(".tf")]
    if not tf_files:
        print("Error: No .tf files found in current directory.")
        print("OpenTofu does not search parent directories. Please run from the directory containing your .tf files.")
        return

    state_file = "terraform.tfstate"

    # Step 1: Parse state
    print("=== Step 1: Parse state file ===")
    state_resources = parse_state_file(state_file)
    print(f"  {len(state_resources)} resources in state")

    # Step 2: Discover cluster
    print("\n=== Step 2: Discover K8s cluster ===")
    cluster = discover_cluster_resources()
    total_k8s = sum(len(v) for v in cluster.values())
    print(f"  {total_k8s} K8s resources discovered")

    # Step 3: Get planned resources and evaluated metadata.name from tofu plan
    print("\n=== Step 3: Extract from tofu plan ===")
    planned_resources, metadata_names, plan_summary, manifest_info, plan_data = get_planned_resources()
    print(f"  {len(planned_resources)} kubernetes resources in plan")
    print(f"  {len(metadata_names)} with evaluated metadata.name")
    print(f"  {len(manifest_info)} manifest resources with full info")

    # Step 3b: Get helm releases
    print("\n=== Step 3b: Extract helm releases ===")
    helm_releases = get_helm_releases()
    print(f"  {len(helm_releases)} helm releases in plan")

    # Step 4: Find missing resources (in plan ∧ K8s) - state
    print("\n=== Step 4: Find missing resources ===")
    to_import = []
    ambiguous = []
    helm_needs_reconcile = []  # Track helm releases needing special handling

    for addr in sorted(planned_resources):
        # Strip index suffix for state comparison (state file doesn't have indices)
        addr_base = addr.split("[")[0]
        if addr_base in state_resources:
            continue
        k8s_id = address_to_k8s_id(addr, cluster, metadata_names, manifest_info)
        if k8s_id is None:
            print(f"  [NO_MATCH]  {addr}")
        elif isinstance(k8s_id, list):
            print(f"  [AMBIGUOUS] {addr}")
            for i, (cand_id, match_type) in enumerate(k8s_id, 1):
                print(f"    [{i}] {cand_id} ({match_type})")
            ambiguous.append((addr, k8s_id))
        else:
            to_import.append((addr, k8s_id))
            print(f"  [MISSING]   {addr} → {k8s_id}")

    # Also handle helm releases
    # NOTE: helm_release imports are problematic because the helm provider cannot
    # import releases that weren't originally created by it. The import command fails
    # with "release: not found" even when the release exists in the cluster.
    #
    # Solution: For helm releases that exist in the cluster but not in state,
    # we need to either:
    # 1. Delete the helm release secret, then run `tofu apply` to recreate it
    # 2. Use `helm upgrade --force-recreate` to let tofu manage it
    #
    # We'll detect this case and offer automatic remediation.
    for addr, (name, namespace) in helm_releases.items():
        # Strip index suffix for state comparison
        addr_base = addr.split("[")[0]
        if addr_base in state_resources:
            continue

        # Check if helm release secret exists in the cluster
        check_cmd = f"kubectl get secret sh.helm.release.v1.{name}.v1 -n {namespace} 2>/dev/null"
        _, output, rc = run_cmd(check_cmd, timeout=10)

        if rc == 0:
            # Release exists in cluster but not in tofu state
            # We need to delete the secret and let tofu recreate it
            print(f"  [HELM:EXISTS] {addr} exists in cluster but not in state")
            print("             Will delete secret and re-apply to reconcile")

            # Add to a list for later remediation
            helm_needs_reconcile.append((addr, name, namespace))
        else:
            # Release doesn't exist, tofu can create it normally
            print(f"  [MISSING]   {addr} → {name}/{namespace} (will be created by apply)")

    # Handle ambiguous resources with interactive selection
    while ambiguous:
        addr, candidates = ambiguous.pop(0)
        print(f"\n  Ambiguous match for [{addr}]:")
        for i, (cand_id, match_type) in enumerate(candidates, 1):
            print(f"    [{i}] {cand_id} ({match_type})")
        try:
            choice = input(f"  Select [1-{len(candidates)}] or [s]kip: ").strip()
        except EOFError:
            choice = "s"
        if choice.lower() == "s":
            print(f"  [SKIPPED]   {addr}")
            continue
        try:
            idx = int(choice) - 1
            if 0 <= idx < len(candidates):
                selected = candidates[idx][0]
                to_import.append((addr, selected))
                print(f"  [MISSING]   {addr} → {selected}")
            else:
                print("  Invalid choice, skipping")
        except ValueError:
            print("  Invalid input, skipping")

    # Step 5: Import resources (sorted by address)
    to_import.sort(key=lambda x: x[0])
    print(f"\n=== Step 5: Import {len(to_import)} resources ===\n")
    success, failed = 0, 0

    for addr, k8s_id in to_import:
        cmd = f"tofu import '{addr}' '{k8s_id}'"
        _, stderr, rc = run_cmd(cmd, timeout=120)
        if rc == 0:
            print(f"  [OK]    {addr}")
            success += 1
        else:
            err = stderr.strip().split("\n")[-1][:60] if stderr else "unknown"
            print(f"  [FAIL]  {addr}: {err}")
            failed += 1

    print(f"\n=== Result: {success} success, {failed} failed ===")

    # Step 5b: Restore random_password resources
    # Kubernetes secrets (e.g., kubernetes_secret_v1.ragflow_env) reference random_password resources.
    # If random_password is not in state but the secret is, tofu apply will generate NEW
    # random passwords, which would overwrite the secret with new values.
    # This breaks existing deployments because they rely on the old passwords stored in secrets.
    # Example chain: kubernetes_deployment_v1.ragflow -> kubernetes_secret_v1.ragflow_env -> random_password.mysql
    if success > 0:
        state_resources = parse_state_file(state_file)
    print("\n=== Step 5b: Restore random_password resources ===")
    restore_random_password_state(state_file, plan_data, state_resources)

    # Step 6: Reconcile helm releases
    # For helm releases that exist in the cluster but not in state, we need to
    # delete the helm release secret and let tofu recreate it
    if helm_needs_reconcile:
        print(f"\n=== Step 6: Reconcile {len(helm_needs_reconcile)} helm releases ===")
        print("  Deleting helm release secrets...")
        print("  (This is required because helm provider cannot import existing releases)")
        print("  NOTE: Please run 'tofu apply' manually after this script completes.")

        for addr, name, namespace in helm_needs_reconcile:
            secret_name = f"sh.helm.release.v1.{name}.v1"
            print(f"  Deleting secret {secret_name} in namespace {namespace}...")
            delete_cmd = f"kubectl delete secret {secret_name} -n {namespace} --ignore-not-found"
            _, delete_output, delete_rc = run_cmd(delete_cmd, timeout=30)
            if delete_rc == 0:
                print(f"    Deleted {secret_name}")
            else:
                print(f"    Failed to delete: {delete_output}")

        print("\n  Helm release secrets deleted.")
        print("  Please run 'tofu apply' manually to recreate the helm releases.")

    # Verify: run plan again and show summary
    print("\n=== Verify: tofu plan summary ===")
    _, _, plan_summary_verify, _, _ = get_planned_resources()
    total = sum(len(v) for v in plan_summary_verify.values())
    for action, addrs in plan_summary_verify.items():
        if addrs:
            print(f"  {action.upper()}: {len(addrs)}")
            for addr in sorted(addrs):
                print(f"    {addr}")
    print(f"\n  Total: {total} changes")


if __name__ == "__main__":
    main()
