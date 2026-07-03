#!/usr/bin/env python3
"""
Resolve cluster-scoped resource ownership for BYOK deployments.

External data source input (stdin JSON):
  - kubeconfig_path: path to kubeconfig
  - cloud_provider: cloud provider name
  - state_path: path to local OpenTofu state file

External data source output (stdout JSON string map):
  - manage_cluster_scoped_resources: "true" or "false"
  - decision_reason: short reason for the decision
"""

from __future__ import annotations

import json
import os
import subprocess
import sys
from pathlib import Path


def read_query() -> dict[str, str]:
    raw = sys.stdin.read().strip()
    if not raw:
        return {}
    payload = json.loads(raw)
    if not isinstance(payload, dict):
        raise ValueError("external query must be a JSON object")
    return {str(k): str(v) for k, v in payload.items()}


def state_tracks_resource(state_path: str, resource_type: str, resource_name: str) -> bool:
    path = Path(state_path)
    if not path.exists() or path.stat().st_size == 0:
        return False

    try:
        state = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return False

    resources = state.get("resources")
    if not isinstance(resources, list):
        return False

    for resource in resources:
        if not isinstance(resource, dict):
            continue
        if resource.get("type") == resource_type and resource.get("name") == resource_name:
            return True
    return False


def kubectl_exists(args: list[str], kubeconfig_path: str) -> bool:
    env = os.environ.copy()
    if kubeconfig_path:
        env["KUBECONFIG"] = kubeconfig_path

    result = subprocess.run(
        ["kubectl", "get", *args],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        env=env,
        check=False,
    )
    return result.returncode == 0


def shared_eck_installed(kubeconfig_path: str) -> bool:
    has_crd = kubectl_exists(["crd", "elasticsearches.elasticsearch.k8s.elastic.co"], kubeconfig_path)
    has_operator = kubectl_exists(["statefulset", "elastic-operator", "-n", "elastic-system"], kubeconfig_path) or kubectl_exists(
        ["deployment", "elastic-operator", "-n", "elastic-system"], kubeconfig_path
    )
    return has_crd and has_operator


def main() -> None:
    query = read_query()
    kubeconfig_path = query.get("kubeconfig_path", "")
    cloud_provider = query.get("cloud_provider", "")
    state_path = query.get("state_path", str(Path.cwd() / "terraform.tfstate"))
    workspace = os.environ.get("TF_WORKSPACE", "default")
    if workspace != "default":
        state_path = str(Path.cwd() / "terraform.tfstate.d" / workspace / "terraform.tfstate")

    state_owns_eck = state_tracks_resource(state_path, "helm_release", "eck_operator")
    state_owns_compute_class = cloud_provider == "gcp" and state_tracks_resource(state_path, "kubernetes_manifest", "elasticsearch_compute_class")
    state_owns_cluster_scoped_resources = state_owns_eck or state_owns_compute_class

    if state_owns_cluster_scoped_resources:
        result = {
            "manage_cluster_scoped_resources": "true",
            "decision_reason": "state_owns_cluster_scoped_resources",
        }
        print(json.dumps(result))
        return

    if shared_eck_installed(kubeconfig_path):
        result = {
            "manage_cluster_scoped_resources": "false",
            "decision_reason": "shared_eck_detected_reuse_cluster_scoped_resources",
        }
        print(json.dumps(result))
        return

    result = {
        "manage_cluster_scoped_resources": "true",
        "decision_reason": "shared_eck_not_detected_bootstrap_cluster_scoped_resources",
    }
    print(json.dumps(result))


if __name__ == "__main__":
    main()
