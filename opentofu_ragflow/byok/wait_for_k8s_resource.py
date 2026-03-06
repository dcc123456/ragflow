#!/usr/bin/env python3
"""
Kubernetes Resource Wait Utility

Wait for Kubernetes resources with exponential backoff retry.

Usage:
    python3 wait_for_k8s_resource.py <namespace> <resource_type> <resource_name>

Examples:
    # Wait for a secret
    python3 wait_for_k8s_resource.py ragflow secret elasticsearch-es-elastic-user

    # Wait for a CRD to be established
    python3 wait_for_k8s_resource.py default crd elasticsearches.elasticsearch.k8s.elastic.co

    # Wait for a specific pod to be ready (by pod name)
    python3 wait_for_k8s_resource.py ragflow pod ragflow-12345-abcde

    # Wait for pods matching label selector (use --selector flag)
    python3 wait_for_k8s_resource.py ragflow pod --selector=app=ragflow

Environment Variables (optional):
    TIMEOUT_SECONDS: Maximum time to wait (default: 300)
    INITIAL_DELAY: Initial retry delay in seconds (default: 2)
    MAX_DELAY: Maximum retry delay in seconds (default: 30)
    KUBECONFIG: Path to kubeconfig file
"""

import argparse
import os
import sys
import subprocess
import time

# Note: Output buffering disabled by using print() + flush() for real-time display


def run_kubectl(args):
    """Run kubectl command, return (success, stdout)"""
    cmd = ['kubectl'] + args
    try:
        result = subprocess.run(cmd, capture_output=True, text=True, timeout=30)
        return result.returncode == 0, result.stdout
    except subprocess.TimeoutExpired:
        return False, ""
    except FileNotFoundError:
        print("ERROR: kubectl not found in PATH", file=sys.stderr)
        sys.exit(1)


def wait_for_resource(namespace, resource_type, name, timeout, initial_delay, max_delay):
    """
    Unified wait function for ALL Kubernetes resources

    Args:
        namespace: Kubernetes namespace
        resource_type: Type of resource (secret, crd, pod)
        name: Resource name or selector (e.g., --selector=app=ragflow)
        timeout: Maximum wait time in seconds
        initial_delay: Initial retry delay
        max_delay: Maximum retry delay
    """
    # Unified resource configuration with lambda commands (no extra args!)
    resource_configs = {
        'secret': {
            'description': f"secret '{name}' in namespace '{namespace}'",
            'check_cmd': lambda n: ['get', 'secret', n, '-n', namespace, '--ignore-not-found'],
            'exists_check': lambda success, stdout: success and stdout.strip(),
            'ready_msg': f"Secret '{name}' is ready",
            'timeout_msg': f"Timeout: Secret '{name}' not found",
        },
        'crd': {
            'description': f"CRD '{name}' to be established",
            'check_cmd': lambda n: ['get', 'crd', n, '--ignore-not-found'],
            'wait_cmd': lambda n: ['wait', '--for=condition=established', f'crd/{n}', '--timeout=120s'],
            'exists_check': lambda success, stdout: success and stdout.strip(),
            'ready_after_check': True,
            'ready_msg': f"CRD '{name}' is established",
            'timeout_msg': f"Timeout: CRD '{name}' not established",
        },
        'pod': {
            'description': f"pod '{name}' in namespace '{namespace}'",
            'validate': lambda n: n.startswith('-l') or (n.startswith('--selector') and '=' not in n),
            'error_hint': "Label selectors MUST use --selector=app=ragflow format",
            'wait_cmd': lambda n: (
                ['wait', '--for=condition=ready', 'pod', n, '-n', namespace, '--timeout=30s']
                if n.startswith('--selector')
                else ['wait', '--for=condition=ready', f'pod/{n}', '-n', namespace, '--timeout=30s']
            ),
            'ready_msg': f"Pod '{name}' is ready",
            'timeout_msg': f"Timeout: Pod '{name}' not ready",
        },
    }

    if resource_type not in resource_configs:
        print(f"ERROR: Unknown resource type: {resource_type}", file=sys.stderr)
        sys.exit(1)

    config = resource_configs[resource_type]

    # Validate if validation function exists
    if 'validate' in config:
        if config['validate'](name):
            print(f"ERROR: Invalid {resource_type} selector: '{name}'", file=sys.stderr)
            print(f"ERROR: {config['error_hint']}", file=sys.stderr)
            print("ERROR:   Correct: --selector=app=ragflow", file=sys.stderr)
            print("ERROR:   Incorrect: -l app=ragflow (argparse only captures '-l')", file=sys.stderr)
            sys.exit(1)

    # Print start message directly (no logger buffering)
    print(f"Waiting for {config['description']} (timeout: {timeout}s)")
    sys.stdout.flush()

    start = time.time()
    delay = initial_delay
    resource_found = False

    while time.time() - start < timeout:
        # Phase 1: Check if resource exists (if check_cmd defined)
        if 'check_cmd' in config and not resource_found:
            success, stdout = run_kubectl(config['check_cmd'](name))

            if 'exists_check' in config:
                if config['exists_check'](success, stdout):
                    if config.get('ready_after_check'):
                        resource_found = True
                        print(f"{resource_type.capitalize()} '{name}' found, waiting for {'establishment' if resource_type == 'crd' else 'readiness'}...")
                        sys.stdout.flush()
                    else:
                        print(f"✓ {config['ready_msg']}")
                        sys.stdout.flush()
                        return True
            else:
                time.sleep(delay)
                delay = min(delay * 1.5, max_delay)
                continue

        # Phase 2: Wait for resource condition (if wait_cmd defined)
        if 'wait_cmd' in config and resource_found:
            success, _ = run_kubectl(config['wait_cmd'](name))
            if success:
                print(f"✓ {config['ready_msg']}")
                sys.stdout.flush()
                return True
        elif 'wait_cmd' in config and 'check_cmd' not in config:
            # For pod: no pre-check, just wait
            success, _ = run_kubectl(config['wait_cmd'](name))
            if success:
                print(f"✓ {config['ready_msg']}")
                sys.stdout.flush()
                return True

        time.sleep(delay)
        delay = min(delay * 1.5, max_delay)

    # Timeout
    elapsed = int(time.time() - start)
    print(f"ERROR: {config['timeout_msg']} after {elapsed}s", file=sys.stderr)
    sys.exit(1)


def main():
    parser = argparse.ArgumentParser(
        description='Wait for Kubernetes resources with exponential backoff',
        usage='python3 wait_for_k8s_resource.py <namespace> <resource_type> <resource_name>'
    )
    parser.add_argument('namespace', help='Kubernetes namespace')
    parser.add_argument('resource_type', choices=['secret', 'crd', 'pod'],
                       help='Type of resource to wait for')
    parser.add_argument('resource_name', help='Name of the resource (or selector like --selector=app=foo)')
    parser.add_argument('--timeout', type=int, default=int(os.getenv('TIMEOUT_SECONDS', '300')),
                       help='Timeout in seconds (default: 300 or TIMEOUT_SECONDS env var)')
    parser.add_argument('--initial-delay', type=float, default=float(os.getenv('INITIAL_DELAY', '2')),
                       help='Initial retry delay in seconds (default: 2)')
    parser.add_argument('--max-delay', type=float, default=float(os.getenv('MAX_DELAY', '30')),
                       help='Maximum retry delay in seconds (default: 30)')

    args = parser.parse_args()

    # Set KUBECONFIG if provided
    kubeconfig = os.getenv('KUBECONFIG')
    if kubeconfig:
        os.environ['KUBECONFIG'] = kubeconfig

    # ONE unified function for all resource types!!!
    wait_for_resource(
        namespace=args.namespace,
        resource_type=args.resource_type,
        name=args.resource_name,
        timeout=args.timeout,
        initial_delay=args.initial_delay,
        max_delay=args.max_delay
    )


if __name__ == '__main__':
    main()
