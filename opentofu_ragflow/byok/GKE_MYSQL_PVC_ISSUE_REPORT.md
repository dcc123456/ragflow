# GKE Autopilot MySQL Volume Attachment Diagnostics & Resolution Report

**Date**: March 6, 2026
**Environment**: GKE Autopilot
**Component**: MySQL (StatefulSet), RabbitMQ (Deployment with PVC)

---

## 1. Executive Summary

During the deployment of the RAGFlow infrastructure on GKE Autopilot, the MySQL service failed to start. The initialization process stalled at the `ContainerCreating` state for over 8 minutes.

**Root Cause**: The GKE scheduler placed the MySQL pod on a node instance (likely a default burstable or small instance type) that was unable to attach the Persistent Disk (PD) volume due to GKE Autopilot's strict resource/volume policies or a transient issue with that specific node pool's CSI driver integration.

**Resolution**: Implemented a `node_selector` constraint to force the MySQL pod to run on the `n2` machine family. This aligns it with the successful configuration observed in other stable stateful workloads (like Elasticsearch) and bypasses the problematic node pool.

**Generic Code Fix**: The solution was implemented using a flexible Terraform variable (`mysql_node_selector`), allowing different environments (AWS, Azure, On-prem) to use their own selector logic without hardcoding GKE-specific details in the main codebase.

---

## 2. Detailed Issue Analysis

### 2.1 Symptom
The `mysql-0` pod remained in `Pending` or `ContainerCreating` state indefinitely.
`kubectl describe pod` revealed the following critical events:

1.  **Scheduling Success**: The pod was successfully assigned to a node (`gk3-autopilot-cluster-1-nap-...`).
2.  **Attachment Failure**:
    ```text
    Warning  FailedAttachVolume  ...  attachdetach-controller
    AttachVolume.Attach failed for volume "pvc-..." : 
    ControllerPublish not permitted on node ... due to backoff condition
    ```
    This indicates the Google Persistent Disk CSI driver failed to attach the block storage to the node.

### 2.2 Investigation Steps
1.  **Volume Verification**: Confirmed the PVC and PV were bound correctly.
2.  **Node Inspection**: The failing node was part of a dynamic GKE Autopilot node pool.
3.  **Cross-Reference**: Observed that the Elasticsearch cluster (which is heavy and stateful) was running successfully.
    *   *Insight*: The Elasticsearch deployment uses a specific `ComputeClass` resource that explicitly requests `machineFamily: "n2"`.
4.  **Hypothesis**: The default "general purpose" or "system" nodes picked by GKE Autopilot for MySQL (which had no specific constraints) were incompatible with the volume attachment or hitting a hidden quota/limit on that instance type.

### 2.3 Failed Remediation Attempts
*   **Manual Deletion**: Deleting the pod to trigger a reschedule didn't work; it landed on the same or similar usage node.
*   **Hostname Anti-Affinity**: Attempting to patch the Deployment to avoid the specific failing hostname (`kubernetes.io/hostname`) was **blocked** by GKE Autopilot's admission controller (Webhooks), which restricts arbitrary node selection to ensure managed stability.

---

## 3. Resolution Implementation

### 3.1 The Fix
We enforced a node constraint to steer MySQL away from the problematic default pool and towards the stable `n2` pool.

**Applied Configuration (Terraform):**
```hcl
# In main.tf
resource "kubernetes_stateful_set" "mysql" {
  # ...
  spec {
     node_selector = var.mysql_node_selector
  }
}
```

**GKE Specific Configuration (`terraform.tfvars`):**
```hcl
mysql_node_selector = {
  "cloud.google.com/machine-family" = "n2"
}
```

### 3.2 Verification
After applying the Terraform plan:
1.  The `mysql-0` pod was recreated.
2.  GKE Autoscaler triggered a scale-up (`TriggeredScaleUp`) to provision a new `n2` node.
3.  The volume successfully attached to the new node.
4.  MySQL initialization completed successfully.

---

## 4. Risk Assessment: Are other PVCs at risk?

**Verdict**: **Yes, potentially.**

Any workload using **Persistent Volume Claims (PVC)** in GKE Autopilot is susceptible to this "bad node placement" issue if it relies entirely on default scheduling.

### Risk Analysis by Component:

| Component | Resource Type | Storage | Risk Level | Mitigation Status |
| :--- | :--- | :--- | :--- | :--- |
| **MySQL** | StatefulSet | PVC (RWO) | **Fixed** | Explicit `node_selector` ("n2") applied. |
| **Elasticsearch** | StatefulSet (Operator) | PVC (RWO) | **Low** | Already uses `ComputeClass` targeting `n2`, which is stable. |
| **RabbitMQ** | Deployment | PVC (RWO) | **High** | Currently uses default scheduling. If the pod restarts and lands on a "bad" node, it may fail to attach its existing volume. |
| **Redis** | Deployment | Ephemeral? | **Low** | Configured with `emptyDir` or no persistence in current code (based on `main.tf` review). *Correction: `main.tf` review needed to confirm persistence.* |
| **MinIO (S3)** | Deployment/StatefulSet | PVC | **N/A** | Using Cloud Storage (GCS) in this setup, so no Block Storage PVC risk. |

### Recommendation
1.  **RabbitMQ**: Since RabbitMQ also uses a PVC (`kubernetes_persistent_volume_claim.rabbitmq`), it is highly recommended to apply the same `mysql_node_selector` (or a similar `rabbitmq_node_selector`) to the RabbitMQ deployment to prevent future outages during upgrades or node recycles.
2.  **General Policy**: For all stateful workloads (RWO PVCs) on GKE Autopilot, best practice is to explicit define a `machine-family` (e.g., `Balanced` or `MemoryOptimized` / `n2`) to ensure predictable IO/Volume performance and avoid "burstable" node limitations.

---

## 5. Supporting Data & References

### 5.1 Node Pool Observations
Inspection of the node pools confirms the cluster runs a mix of `n2` (General Purpose, stable) and `ek` (E2, likely cost-optimized/burstable) instances. The `n2` nodes created by Autopilot (prefixed `nap-`) are where the fixed workloads reside.

**Verification Command:**
```bash
kubectl get nodes -o custom-columns="NAME:.metadata.name,POOL:.metadata.labels.cloud\.google\.com/gke-nodepool,MACHINE_TYPE:.metadata.labels.node\.kubernetes\.io/instance-type,FAMILY:.metadata.labels.cloud\.google\.com/machine-family,CPU:.status.capacity.cpu,MEM:.status.capacity.memory"
```

**Output:**
```text
NAME                                                 POOL           MACHINE_TYPE     FAMILY   CPU   MEM
gk3-autopilot-cluster-1-nap-14dlvkfl-998a2bbc-mzpr   nap-14dlvkfl   n2-standard-8    n2       8     32869480Ki
gk3-autopilot-cluster-1-nap-1i97yd3n-f1785ecb-jrqc   nap-1i97yd3n   n2-standard-8    n2       8     32869480Ki
gk3-autopilot-cluster-1-pool-1-157fa5e7-qc8h         pool-1         ek-standard-8    ek       8     32869480Ki
gk3-autopilot-cluster-1-pool-1-225e26a7-d7kz         pool-1         ek-standard-8    ek       8     32869472Ki
gk3-autopilot-cluster-1-pool-1-6a8dd2f1-h9qb         pool-1         ek-standard-8    ek       8     32869480Ki
gk3-autopilot-cluster-1-pool-2-19f290d7-rwph         pool-2         ek-standard-16   ek       16    65848308Ki
gk3-autopilot-cluster-1-pool-2-2961a789-g4gs         pool-2         ek-standard-16   ek       16    65848300Ki
```

### 5.2 References
*   **GKE Machine Types**: [Machine families resource comparison](https://docs.cloud.google.com/compute/docs/machine-resource)
*   **GKE Pricing**: [Google Kubernetes Engine Pricing](https://cloud.google.com/kubernetes-engine/pricing)
*   **Pod Performance**: [Optimizing Pod performance on GKE](https://docs.cloud.google.com/kubernetes-engine/docs/how-to/performance-pods)
*   **Custom Compute Classes**: [About custom compute classes in Autopilot](https://docs.cloud.google.com/kubernetes-engine/docs/concepts/about-custom-compute-classes)
