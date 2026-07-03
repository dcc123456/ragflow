#!/usr/bin/env python3
# -*- PEP 723 -*-
# /// script
# requires-python = ">=3.11"
# dependencies = [
#     "kubernetes>=28.0.0",
#     "requests>=2.31.0",
# ]
# ///

"""
GKE SSL Certificate Sync Script

Opentofu creates a Kubernetes Secret to store the SSL certificate beforehand. This script (inside or outside GKE) updates it regularly.

About ohttps Auto-Renewal:
    In ohttps certificate management, you can configure the auto-renewal time before expiration
    (e.g., 15 days before expiry). When the certificate approaches its expiration date, ohttps
    will automatically issue a new certificate. This script fetches the latest certificate
    (including renewed ones), so you don't need to manually trigger renewals.

    Simply run this script periodically (e.g., daily via cron) to keep the certificate in sync.

Reference: https://ohttps.com/docs/cloud/api/api

Usage:
    Set environment variables:
        - OHTTPS_API_ID: ohttps API ID (e.g., push-ny5jx0l55gzr7m6p)
        - OHTTPS_API_KEY: ohttps API Key (e.g., f0dd04ac375688e6f590c0cd143690dd)
        - OHTTPS_CERT_ID: Certificate ID to pull (e.g., cert-9dxel044lw604j7o)
        - CERT_OUTPUT_DIR: Local output directory for certificate files (default: current directory)
        - CERT_FILE_PREFIX: Local file prefix (default: ragflow-tls)
        - SYNC_K8S_SECRET: Whether to sync Kubernetes Secret after download (default: 0)
        - KUBECONFIG: Path to kubeconfig file (optional, uses in-cluster config if not set)
        - SECRET_NAMESPACE: Kubernetes namespace (default: ragflow)
        - SECRET_NAME: TLS secret name (default: ragflow-tls)

Example:
    export OHTTPS_API_ID="push-xxxxxx"
    export OHTTPS_API_KEY="xxxxxxxxxxxxxxxx"
    export OHTTPS_CERT_ID="cert-xxxxxx"
    export CERT_OUTPUT_DIR="."
    export CERT_FILE_PREFIX="ragflow-tls"
    export SYNC_K8S_SECRET="0"
    export SECRET_NAME="ragflow-tls"
    python3 sync_ohttps_cert.py

OHTTPS API 成功响应示例:
{
    "success": true,
    "msg": "成功",
    "payload": {
        "certKey": "-----BEGIN RSA PRIVATE KEY-----\nMIIEogIBAAKCAQEA7o8I0jT1...3balniJJw7zfou0=\n-----END RSA PRIVATE KEY-----\n",
        "fullChainCerts": "-----BEGIN CERTIFICATE-----\nMIIFMzCCBBugAwIBAgISA4qi...YKEBpsr6GtPAQ3ec5\n-----END CERTIFICATE-----\n",
        "expiredTime": "2021-08-09T14:37:10.000Z"
    }
}
"""

import base64
import hashlib
import os
import sys
import time
from datetime import datetime
from pathlib import Path

import kubernetes
import requests


def get_timestamp() -> str:
    """Get current timestamp in milliseconds."""
    return str(int(time.time() * 1000))


def generate_signature(api_id: str, api_key: str, timestamp: str, cert_id: str) -> str:
    """
    Generate MD5 signature for ohttps API authentication.

    Signature method (according to ohttps docs):
    1. Build params list: ["apiId=xxx", "timestamp=xxx", "certificateId=xxx"]
    2. Add apiKey to params: [..., "apiKey=xxx"]
    3. Sort alphabetically
    4. Join with "&": "apiId=xxx&apiKey=xxx&certificateId=xxx&timestamp=xxx"
    5. Calculate MD5 hash (32 lowercase)
    """
    # Build parameter list
    params = [
        f"apiId={api_id}",
        f"apiKey={api_key}",
        f"certificateId={cert_id}",
        f"timestamp={timestamp}",
    ]

    # Sort alphabetically
    params.sort()

    # Join with "&"
    string_for_sign = "&".join(params)

    # Calculate MD5 (32 lowercase)
    signature = hashlib.md5(string_for_sign.encode("utf-8")).hexdigest()

    return signature


def fetch_certificate(api_id: str, api_key: str, cert_id: str) -> dict:
    """
    Fetch certificate from ohttps API.

    API endpoint: GET /api/open/getCertificate
    """
    base_url = "https://www.ohttps.com"

    timestamp = get_timestamp()

    # Generate signature using apiKey as the signing key
    signature = generate_signature(api_id, api_key, timestamp, cert_id)

    # Build final URL (apiKey is NOT included in URL, only in signature)
    url = f"{base_url}/api/open/getCertificate"
    query_params = {
        "sign": signature,
        "apiId": api_id,
        "timestamp": timestamp,
        "certificateId": cert_id,
    }

    print(f"[{datetime.now().isoformat()}] Fetching certificate from ohttps...")
    print(f"[{datetime.now().isoformat()}] API ID: {api_id}, Cert ID: {cert_id}")

    response = requests.get(url, params=query_params, timeout=30)
    response.raise_for_status()

    result = response.json()

    # Handle different response formats
    # Format 1: {"success": true, "payload": {...}}
    # Format 2: {"code": 0, "data": {...}}
    if result.get("success") is not True and result.get("code") != 0:
        error_msg = result.get("message") or result.get("msg") or "Unknown error"
        raise Exception(f"ohttps API error: {error_msg}")

    # Extract data from either 'payload' or 'data'
    data = result.get("payload") or result.get("data", {})
    print(f"[{datetime.now().isoformat()}] Certificate fetched successfully")
    print(f"[{datetime.now().isoformat()}] Certificate expires: {data.get('expiredTime', 'unknown')}")

    return data


def create_or_update_tls_secret(
    namespace: str,
    secret_name: str,
    cert_data: dict,
) -> None:
    """
    Create or update a TLS secret in Kubernetes.
    """
    # Try in-cluster config first, then fall back to kubeconfig
    try:
        kubernetes.config.load_incluster_config()
    except kubernetes.config.ConfigException:
        kubernetes.config.load_kube_config()

    v1 = kubernetes.client.CoreV1Api()

    # Prepare new secret data (must be base64 encoded)
    # TLS secrets require tls.crt and tls.key, plus tls.expired_time for tracking
    new_secret_data = {
        "tls.crt": base64.b64encode(cert_data["fullChainCerts"].encode("utf-8")).decode("utf-8"),
        "tls.key": base64.b64encode(cert_data["certKey"].encode("utf-8")).decode("utf-8"),
        "tls.expired": base64.b64encode(cert_data["expiredTime"].encode("utf-8")).decode("utf-8"),
    }

    # Check if secret content is the same (skip update if unchanged)
    existing_labels = {}
    existing_secret_full = None
    try:
        existing_secret_full = v1.read_namespaced_secret(name=secret_name, namespace=namespace)
        existing_labels = existing_secret_full.metadata.labels or {}
    except kubernetes.client.exceptions.ApiException as e:
        if e.status == 404:
            # Secret doesn't exist - create it
            print(f"[{datetime.now().isoformat()}] Secret '{secret_name}' not found, creating it...")
            metadata = kubernetes.client.V1ObjectMeta(name=secret_name, namespace=namespace, labels={"app": "ragflow"})
            secret = kubernetes.client.V1Secret(api_version="v1", kind="Secret", metadata=metadata, type="kubernetes.io/tls", data=new_secret_data)
            v1.create_namespaced_secret(namespace=namespace, body=secret)
            print(f"[{datetime.now().isoformat()}] Secret '{secret_name}' created successfully")
            return
        else:
            raise

    cert_unchanged = True
    for key, val in new_secret_data.items():
        if existing_secret_full.data.get(key, "") != val:
            cert_unchanged = False
            break

    if cert_unchanged:
        print(f"[{datetime.now().isoformat()}] Secret '{secret_name}' is unchanged, skipping update")
        return

    # Metadata - preserve existing labels
    labels = dict(existing_labels)

    metadata = kubernetes.client.V1ObjectMeta(name=secret_name, namespace=namespace, labels=labels)

    # Secret body
    secret = kubernetes.client.V1Secret(api_version="v1", kind="Secret", metadata=metadata, type="kubernetes.io/tls", data=new_secret_data)

    # Update existing secret
    v1.replace_namespaced_secret(name=secret_name, namespace=namespace, body=secret)
    print(f"[{datetime.now().isoformat()}] Secret '{secret_name}' updated successfully")


def write_certificate_files(cert_data: dict, output_dir: str, file_prefix: str) -> tuple[str, str]:
    """Write certificate and private key to local files.

    Returns:
        Tuple of (cert_file_path, key_file_path).
    """
    out_dir = Path(output_dir).resolve()
    out_dir.mkdir(parents=True, exist_ok=True)

    cert_path = out_dir / f"{file_prefix}.crt"
    key_path = out_dir / f"{file_prefix}.key"
    expired_path = out_dir / f"{file_prefix}.expired"

    cert_path.write_text(cert_data["fullChainCerts"], encoding="utf-8")
    key_path.write_text(cert_data["certKey"], encoding="utf-8")
    expired_path.write_text(cert_data.get("expiredTime", ""), encoding="utf-8")

    print(f"[{datetime.now().isoformat()}] Wrote certificate to: {cert_path}")
    print(f"[{datetime.now().isoformat()}] Wrote private key to: {key_path}")
    print(f"[{datetime.now().isoformat()}] Wrote expiry time to: {expired_path}")

    return str(cert_path), str(key_path)


def main():
    """Main entry point."""
    # Load configuration from environment variables
    api_id = os.environ.get("OHTTPS_API_ID")
    api_key = os.environ.get("OHTTPS_API_KEY")
    cert_id = os.environ.get("OHTTPS_CERT_ID")

    output_dir = os.environ.get("CERT_OUTPUT_DIR", ".")
    file_prefix = os.environ.get("CERT_FILE_PREFIX", "ragflow-tls")
    sync_k8s_secret = os.environ.get("SYNC_K8S_SECRET", "0").lower() in {"1", "true", "yes", "on"}

    namespace = os.environ.get("SECRET_NAMESPACE", "ragflow")
    secret_name = os.environ.get("SECRET_NAME", "ragflow-tls")

    # Validate required parameters
    missing = []
    if not api_id:
        missing.append("OHTTPS_API_ID")
    if not api_key:
        missing.append("OHTTPS_API_KEY")
    if not cert_id:
        missing.append("OHTTPS_CERT_ID")

    if missing:
        print(f"Error: Missing required environment variables: {', '.join(missing)}", file=sys.stderr)
        print(__doc__, file=sys.stderr)
        sys.exit(1)

    try:
        # Fetch certificate from ohttps
        cert_data = fetch_certificate(api_id, api_key, cert_id)

        # Always write local files so tofu can load ragflow-tls.crt/ragflow-tls.key directly.
        write_certificate_files(
            cert_data=cert_data,
            output_dir=output_dir,
            file_prefix=file_prefix,
        )

        # Optionally sync Kubernetes Secret for cluster-side usage.
        if sync_k8s_secret:
            create_or_update_tls_secret(
                namespace=namespace,
                secret_name=secret_name,
                cert_data=cert_data,
            )
        else:
            print(f"[{datetime.now().isoformat()}] Skipping Kubernetes Secret sync (SYNC_K8S_SECRET=0)")

        print(f"[{datetime.now().isoformat()}] SSL certificate sync completed successfully")
        sys.exit(0)

    except Exception as e:
        print(f"[{datetime.now().isoformat()}] Error: {str(e)}", file=sys.stderr)
        sys.exit(1)


if __name__ == "__main__":
    main()
