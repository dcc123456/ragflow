#!/usr/bin/env python3
"""Get GKE Gateway IP from GCP annotations with retry."""

import json
import subprocess
import sys
import time


def wait_for_gateway_programmed(retries=60, delay=10):
    """Wait for Gateway Programmed condition to be True."""
    cmd = ["kubectl", "get", "gateway", "ragflow", "-n", "ragflow", "-o", "jsonpath={.status.conditions[?(@.type=='Programmed')].status}"]
    for attempt in range(retries):
        result = subprocess.run(cmd, capture_output=True, text=True)
        if result.returncode == 0 and result.stdout.strip() == "True":
            return True
        time.sleep(delay)
    return False


def get_gateway_ip(retries=30, delay=2):
    """Get the annotated address from Gateway CR, with retry for Pending state."""
    cmd = ["kubectl", "get", "gateway", "ragflow", "-n", "ragflow", "-o", "json"]

    for attempt in range(retries):
        result = subprocess.run(cmd, capture_output=True, text=True)
        if result.returncode != 0:
            if attempt < retries - 1:
                time.sleep(delay)
                continue
            print(json.dumps({"address": ""}))
            sys.exit(0)

        data = json.loads(result.stdout)
        addr = data.get("metadata", {}).get("annotations", {}).get("networking.gke.io/addresses", "")

        if not addr:
            if attempt < retries - 1:
                time.sleep(delay)
                continue
            print(json.dumps({"address": ""}))
            sys.exit(0)

        # Parse address resource name and region from path like:
        # projects/PROJECT/regions/REGION/addresses/NAME
        parts = addr.split("/")
        addr_name = parts[-1]
        region = parts[-3]

        # Get actual IP from GCP
        cmd_ip = ["gcloud", "compute", "addresses", "describe", addr_name, "--region", region, "--format=get(address)"]
        result_ip = subprocess.run(cmd_ip, capture_output=True, text=True)
        ip = result_ip.stdout.strip() if result_ip.returncode == 0 else ""

        print(json.dumps({"address": ip}))
        sys.exit(0)

    print(json.dumps({"address": ""}))


if __name__ == "__main__":
    # Wait for Gateway to be programmed first
    wait_for_gateway_programmed()
    get_gateway_ip()
