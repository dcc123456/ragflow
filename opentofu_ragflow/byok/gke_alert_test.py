import subprocess
import time
import json
import os
import sys
import datetime
import urllib.request
import urllib.error

GCP_ALERT_EMAIL = "yuzhichang@gmail.com"


def run_cmd(cmd):
    print(f"[{datetime.datetime.now().strftime('%H:%M:%S')}] Running: {cmd}")
    try:
        return subprocess.run(cmd, shell=True, capture_output=True, text=True, check=True)
    except subprocess.CalledProcessError as e:
        print(f"Command failed: {cmd}\nExit code: {e.returncode}\nStderr: {e.stderr}")
        sys.exit(e.returncode)


def main():
    os.environ["GCP_ALERT_EMAIL"] = GCP_ALERT_EMAIL
    print(f"Set GCP_ALERT_EMAIL={GCP_ALERT_EMAIL}")

    # Ensure we can import the gke_setup script safely based on current script path
    base_dir = os.path.dirname(os.path.abspath(__file__))
    script_dir = os.path.join(base_dir, "opentofu_ragflow", "byok")
    sys.path.append(script_dir)
    try:
        import gke_setup
    except ImportError:
        print("Could not import gke_setup. Please verify the path.")
        return

    project_result = run_cmd("gcloud config get-value project")
    project = project_result.stdout.strip()
    if not project:
        print("Failed to get GCP project.")
        return
    print(f"Project: {project}")

    print("\n--- Step 1: Setting up monitoring alerts ---")
    gke_setup.setup_monitoring_alerts(project)

    print("\n--- Step 2: Restarting MySQL (Triggering Event) ---")
    start_time_utc = datetime.datetime.now(datetime.timezone.utc).replace(tzinfo=None)
    start_time_str = start_time_utc.isoformat("T") + "Z"
    # Idempotent restart using rollout restart
    run_cmd("kubectl rollout restart statefulset mysql -n ragflow")

    wait_minutes = 3  # 3 minutes should be enough for metrics to appear in the API
    print(f"\n--- Step 3: Waiting for {wait_minutes} minutes for metrics to process ---")
    for i in range(wait_minutes, 0, -1):
        print(f"[{datetime.datetime.now().strftime('%H:%M:%S')}] {i} minutes remaining...")
        time.sleep(60)

    print("\n--- Step 4: Checking Log Entries ---")
    # Get the exact filter dynamically to ensure consistency
    filter_query = f'{gke_setup.POD_STATUS_CHANGE_FILTER} timestamp>="{start_time_str}"'
    log_cmd = f"gcloud logging read '{filter_query}' --limit=5 --format=json"
    log_result = run_cmd(log_cmd)
    try:
        logs = json.loads(log_result.stdout)
        print(f"Found {len(logs)} log entries matching the criteria.")
        for log in logs:
            payload = log.get("jsonPayload", {})
            message = payload.get("message", "")
            reason = payload.get("reason", "")
            inv_obj = payload.get("involvedObject", {}).get("name", "unknown")
            print(f" - Pod {inv_obj}: [{reason}] {message}")
    except Exception:
        print("Failed to parse logs or no logs found.")

    print("\n--- Step 5: Checking Metric 'pod_status_change_events' API ---")
    token_res = subprocess.run("gcloud auth print-access-token", shell=True, capture_output=True, text=True)
    if token_res.returncode == 0:
        token = token_res.stdout.strip()
        end_time = datetime.datetime.now(datetime.timezone.utc).replace(tzinfo=None).isoformat("T") + "Z"
        url = f"https://monitoring.googleapis.com/v3/projects/{project}/timeSeries?filter=metric.type%3D%22logging.googleapis.com/user/pod_status_change_events%22&interval.endTime={end_time}&interval.startTime={start_time_str}"

        req = urllib.request.Request(url, headers={"Authorization": f"Bearer {token}"})
        try:
            with urllib.request.urlopen(req) as response:
                res_body = response.read().decode("utf-8")
                res_json = json.loads(res_body)
                time_series = res_json.get("timeSeries", [])
                total_points = 0
                for ts in time_series:
                    points = ts.get("points", [])
                    total_points += sum(int(p.get("value", {}).get("int64Value", 0)) for p in points)

                print(f"Found {len(time_series)} time series streams for 'pod_status_change_events'.")
                if total_points > 0:
                    print(f"[SUCCESS] The metric recorded {total_points} events since the restart!")
                else:
                    print("[WARNING] The metric has no recorded values > 0 in the queried interval. It might take longer.")
        except Exception as e:
            print(f"Failed to query monitoring API: {e}")
    else:
        print("Could not get gcloud auth token to query monitoring API.")

    print("\n--- Testing finished. Check output for metrics increment and check email for alerts. ---\n")


if __name__ == "__main__":
    main()
