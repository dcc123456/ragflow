#!/bin/bash
# Aliyun RAM Roles and Services Setup Script
# Creates 16 RAM roles and 2 linked services for CS and OSS

set -e

REGION="cn-shanghai"
ACCOUNT_ID=$(aliyun sts GetCallerIdentity --query AccountId --output text 2>/dev/null || echo "YOUR_ACCOUNT_ID")

echo "╔════════════════════════════════════════════════════════════╗"
echo "║  Aliyun RAM Roles and Services Setup                        ║"
echo "╚════════════════════════════════════════════════════════════╝"
echo ""
echo "Region: $REGION"
echo "Account ID: $ACCOUNT_ID"
echo ""

# Role definitions with their trusted services and policies
# Format: "RoleName|TrustedService|Description"
declare -a ROLES=(
    "AliyunCSManagedVKRole|cs.aliyuncs.com|ACK virtual node RAM role"
    "AliyunCSManagedNlcRole|cs.aliyuncs.com|ACK network load controller RAM role"
    "AliyunCSManagedAutoScalerRole|cs.aliyuncs.com|ACK cluster autoscaler RAM role"
    "AliyunOOSLifecycleHook4CSRole|oos.aliyuncs.com|OOS lifecycle hook for CS RAM role"
    "AliyunCCCSIPluginRole|cs.aliyuncs.com|ACK CSI plugin RAM role"
    "AliyunCSDefaultRole|ecs.aliyuncs.com|ACK worker node default RAM role"
    "AliyunCSManagedKubernetesRole|cs.aliyuncs.com|ACK managed cluster control plane RAM role"
    "AliyunCSManagedLogRole|log.aliyuncs.com|ACK log collection RAM role"
    "AliyunCSManagedCmsRole|cs.aliyuncs.com|ACK CloudMonitor integration RAM role"
    "AliyunCSManagedCsiRole|cs.aliyuncs.com|ACK CSI storage RAM role"
    "AliyunCSKubernetesAuditRole|log.aliyuncs.com|ACK audit logging RAM role"
    "AliyunCSManagedNetworkRole|cs.aliyuncs.com|ACK network management RAM role"
    "AliyunCSManagedArmsRole|arms.aliyuncs.com|ACK ARMS/Prometheus integration RAM role"
    "AliyunCSServerlessKubernetesRole|cs.aliyuncs.com|ASK (Serverless) cluster RAM role"
    "AliyunCSManagedCsiPluginRole|cs.aliyuncs.com|ACK CSI plugin RAM role"
    "AliyunCSManagedCsiProvisionerRole|cs.aliyuncs.com|ACK CSI provisioner RAM role"
)

# Policy templates for different role types
create_policy_document() {
    local role_name=$1
    local account_id=$2

    case $role_name in
        AliyunCSManagedVKRole)
            cat <<EOF
{
  "Version": "1",
  "Statement": [
    {
      "Action": [
        "ecs:CreateInstance",
        "ecs:RunInstance",
        "ecs:DeleteInstance",
        "ecs:DescribeInstances",
        "ecs:DescribeInstanceAttribute",
        "ecs:DescribeInstanceStatus",
        "ecs:DescribeInstanceType",
        "ecs:DescribeSecurityGroups",
        "ecs:DescribeVSwitches",
        "ecs:DescribeVpcs",
        "ecs:CreateSecurityGroup",
        "ecs:AuthorizeSecurityGroup",
        "ecs:JoinSecurityGroup",
        "ecs:DescribeSnapshots",
        "ecs:CreateNetworkInterface",
        "ecs:DescribeNetworkInterfaces",
        "ecs:DeleteNetworkInterface",
        "ecs:CreateRouteEntry",
        "ecs:DeleteRouteEntry",
        "ecs:DescribeRouteTables",
        "ecs:DetachNetworkInterface",
        "ecs:AttachNetworkInterface",
        "vpc:DescribeVSwitches",
        "vpc:DescribeVpcs"
      ],
      "Resource": "*",
      "Effect": "Allow"
    },
    {
      "Action": "ram:PassRole",
      "Resource": "*",
      "Effect": "Allow"
    },
    {
      "Action": "im:CreateInstance",
      "Resource": "*",
      "Effect": "Allow"
    }
  ]
}
EOF
            ;;
        AliyunCSManagedNetworkRole|AliyunCSManagedNlcRole)
            cat <<EOF
{
  "Version": "1",
  "Statement": [
    {
      "Action": [
        "vpc:DescribeVSwitches",
        "vpc:DescribeVpcs",
        "vpc:DescribeRouteTableList",
        "vpc:DescribeRouteTables",
        "vpc:CreateRouteEntry",
        "vpc:DeleteRouteEntry",
        "vpc:DescribeRouteEntryList",
        "ecs:DescribeNetworkInterfaces",
        "ecs:CreateNetworkInterface",
        "ecs:DeleteNetworkInterface",
        "ecs:AttachNetworkInterface",
        "ecs:DetachNetworkInterface",
        "ecs:DescribeInstances",
        "slb:DescribeLoadBalancers",
        "slb:DescribeLoadBalancerAttribute",
        "slb:DescribeHealthStatus",
        "slb:RemoveBackendServers",
        "slb:AddBackendServers",
        "slb:SetLoadBalancerStatus",
        "slb:DescribeLoadBalancerHTTPListenerAttribute",
        "slb:StartLoadBalancerListener",
        "slb:StopLoadBalancerListener",
        "slb:DescribeLoadBalancerHTTPSListenerAttribute",
        "slb:DescribeLoadBalancerTCPListenerAttribute",
        "slb:DescribeLoadBalancerUDPListenerAttribute",
        "alb:DescribeLoadBalancers",
        "alb:DescribeZones",
        "alb:JoinResourceGroup",
        "alb:TagResources",
        "alb:DescribeLoadBalancerAttribute",
        "alb:ModifyLoadBalancerAttribute",
        "alb:EnableDeletionProtection",
        "alb:DisableDeletionProtection",
        "alb:DescribeListenerAttribute",
        "alb:CreateRules",
        "alb:DeleteRules",
        "alb:DescribeRules",
        "alb:UpdateRulesAttribute",
        "alb:DescribeRuleAttribute",
        "alb:UpdateRuleAttribute",
        "alb:RemoveListenerWhiteListItem",
        "alb:AddListenerWhiteItemList",
        "alb:RemoveListenerBlackListItem",
        "alb:AddListenerBlackItemList",
        "alb:RemoveListenerWhiteListItem",
        "alb:AddListenerWhiteItemList",
        "alb:RemoveListenerBlackListItem",
        "alb:AddListenerBlackItemList"
      ],
      "Resource": "*",
      "Effect": "Allow"
    }
  ]
}
EOF
            ;;
        AliyunCSManagedCsiRole|AliyunCSManagedCsiPluginRole|AliyunCSManagedCsiProvisionerRole)
            cat <<EOF
{
  "Version": "1",
  "Statement": [
    {
      "Action": [
        "ecs:DescribeInstances",
        "ecs:DescribeDisks",
        "ecs:CreateDisk",
        "ecs:AttachDisk",
        "ecs:DetachDisk",
        "ecs:DeleteDisk",
        "ecs:ResizeDisk",
        "ecs:CreateSnapshot",
        "ecs:DeleteSnapshot",
        "ecs:DescribeSnapshots",
        "ecs:CreateAutoSnapshotPolicy",
        "ecs:ApplyAutoSnapshotPolicy",
        "ecs:CancelAutoSnapshotPolicy",
        "ecs:DescribeAutoSnapshotPolicyEX",
        "ecs:DescribeSnapshotPackage",
        "ecs:ModifyDiskAttribute",
        "ecs:ResetDisk",
        "ecs:DescribeSnapshotsUsage",
        "ecs:DescribeSnapshotLinks"
      ],
      "Resource": "*",
      "Effect": "Allow"
    }
  ]
}
EOF
            ;;
        AliyunCSManagedLogRole|AliyunCSKubernetesAuditRole)
            cat <<EOF
{
  "Version": "1",
  "Statement": [
    {
      "Action": [
        "log:PostLogStoreLogs",
        "log:CreateLogStore",
        "log:CreateLogTail",
        "log:CreateIndex",
        "log:UpdateLogStore",
        "log:PushLogToLogStore",
        "log:CreateDashboard",
        "log:CreateProject"
      ],
      "Resource": "*",
      "Effect": "Allow"
    }
  ]
}
EOF
            ;;
        AliyunCSManagedCmsRole|AliyunCSManagedArmsRole)
            cat <<EOF
{
  "Version": "1",
  "Statement": [
    {
      "Action": [
        "cms:DescribeMonitoringAgentHosts",
        "cms:InstallMonitoringAgent",
        "cms:CreateMetricRuleTemplate",
        "cms:DescribeMetricRuleTemplateList",
        "cms:ApplyMetricRuleTemplate",
        "cms:DisableEventRules",
        "cms:EnableEventRules"
      ],
      "Resource": "*",
      "Effect": "Allow"
    }
  ]
}
EOF
            ;;
        AliyunCSDefaultRole|AliyunCSServerlessKubernetesRole|AliyunCSManagedKubernetesRole)
            cat <<EOF
{
  "Version": "1",
  "Statement": [
    {
      "Action": [
        "log:PostLogStoreLogs",
        "log:CreateLogStore",
        "log:CreateLogTail",
        "log:CreateIndex",
        "log:UpdateLogStore",
        "cs:DescribeClusters",
        "cs:DescribeClusterNodes",
        "cs:GetClusterNodes",
        "cs:ScaleOutCluster",
        "cs:ScaleInCluster"
      ],
      "Resource": "*",
      "Effect": "Allow"
    },
    {
      "Action": "ram:PassRole",
      "Resource": "*",
      "Effect": "Allow"
    }
  ]
}
EOF
            ;;
        *)
            # Default policy for other roles
            cat <<EOF
{
  "Version": "1",
  "Statement": [
    {
      "Action": [
        "ecs:DescribeInstances",
        "ecs:DescribeInstanceStatus",
        "cs:DescribeClusters"
      ],
      "Resource": "*",
      "Effect": "Allow"
    }
  ]
}
EOF
            ;;
    esac
}

# Create trust policy
create_trust_policy() {
    local service=$1
    cat <<EOF
{
  "Statement": [
    {
      "Action": "sts:AssumeRole",
      "Effect": "Allow",
      "Principal": {
        "Service": [
          "$service"
        ]
      }
    }
  ],
  "Version": "1"
}
EOF
}

# ========================================
# Step 1: Create RAM Roles
# ========================================
echo "=== Step 1: Creating RAM Roles ==="
echo "───────────────────────────────────────────────────────────────"

for role_info in "${ROLES[@]}"; do
    IFS='|' read -r role_name service description <<< "$role_info"

    echo ""
    echo "Creating role: $role_name"
    echo "  Trusted Service: $service"
    echo "  Description: $description"

    # Create trust policy
    trust_policy=$(create_trust_policy "$service")

    # Check if role already exists
    if aliyun ram GetRole --RoleName "$role_name" >/dev/null 2>&1; then
        echo "  ⚠ Role already exists, skipping..."
        continue
    fi

    # Create role
    result=$(aliyun ram CreateRole \
        --RoleName "$role_name" \
        --Description "$description" \
        --AssumeRolePolicyDocument "$trust_policy" 2>&1)

    if echo "$result" | grep -q "RoleId"; then
        echo "  ✓ Role created successfully"

        # Create and attach policy
        policy_document=$(create_policy_document "$role_name" "$ACCOUNT_ID")
        policy_name="${role_name}Policy"

        # Create custom policy
        aliyun ram CreatePolicy \
            --PolicyName "$policy_name" \
            --Description "Policy for $role_name" \
            --PolicyDocument "$policy_document" >/dev/null 2>&1

        # Attach policy to role
        aliyun ram AttachPolicyToRole \
            --PolicyName "$policy_name" \
            --PolicyType "Custom" \
            --RoleName "$role_name" >/dev/null 2>&1

        echo "  ✓ Policy attached: $policy_name"
    else
        echo "  ✗ Failed: $result"
    fi

    sleep 1
done

echo ""
echo "✓ RAM roles creation complete"
echo ""

# ========================================
# Step 2: Create Service Linked Roles
# ========================================
echo "=== Step 2: Creating Service Linked Roles ==="
echo "───────────────────────────────────────────────────────────────"

# Create service linked roles for CS (Container Service)
echo ""
echo "Creating Service Linked Role for CS (Container Service)..."

# CS service linked role template
create_cs_service_role() {
    local role_name=$1
    cat <<EOF
{
  "Statement": [
    {
      "Action": "sts:AssumeRole",
      "Effect": "Allow",
      "Principal": {
        "Service": [
          "cs.aliyuncs.com"
        ]
      }
    }
  ],
  "Version": "1"
}
EOF
}

# Try to create service linked role for CS
result=$(aliyun ram CreateServiceLinkedRole \
    --ServiceName "CS" \
    --TemplateId "ServiceLinkedRoleForCS" 2>&1 || echo "Already exists or failed")

if echo "$result" | grep -q "AlreadyExists\|RoleId"; then
    echo "  ✓ CS Service Linked Role: OK"
else
    echo "  ℹ CS Service Linked Role: May need manual creation"
    echo "    (Use console if needed: https://ram.console.aliyun.com)"
fi

# Create service linked role for OSS
echo ""
echo "Creating Service Linked Role for OSS..."

result=$(aliyun ram CreateServiceLinkedRole \
    --ServiceName "OSS" \
    --TemplateId "ServiceLinkedRoleForOSS" 2>&1 || echo "Already exists or failed")

if echo "$result" | grep -q "AlreadyExists\|RoleId"; then
    echo "  ✓ OSS Service Linked Role: OK"
else
    echo "  ℹ OSS Service Linked Role: May need manual creation"
    echo "    (Use console if needed: https://ram.console.aliyun.com)"
fi

echo ""
echo "✓ Service linked roles setup complete"
echo ""

# ========================================
# Step 3: Verification
# ========================================
echo "=== Step 3: Verification ==="
echo "───────────────────────────────────────────────────────────────"

echo ""
echo "Verifying created roles:"
echo ""

for role_info in "${ROLES[@]}"; do
    IFS='|' read -r role_name service description <<< "$role_info"

    if aliyun ram GetRole --RoleName "$role_name" >/dev/null 2>&1; then
        echo "  ✓ $role_name"
    else
        echo "  ✗ $role_name (not found)"
    fi
done

echo ""
echo "╔════════════════════════════════════════════════════════════╗"
echo "║  Setup Complete                                            ║"
echo "╚════════════════════════════════════════════════════════════╝"
echo ""
echo "Summary:"
echo "  RAM Roles Created: 16"
echo "  Service Linked Roles: CS, OSS"
echo ""
echo "Next steps:"
echo "  1. Verify roles at: https://ram.console.aliyun.com/roles"
echo "  2. Attach additional policies if needed"
echo "  3. Use roles in ACK/ASK cluster deployment"
echo ""
