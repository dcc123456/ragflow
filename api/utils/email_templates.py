#
#  Copyright 2025 The InfiniFlow Authors. All Rights Reserved.
#
#  Licensed under the Apache License, Version 2.0 (the "License");
#  you may not use this file except in compliance with the License.
#  You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
#  Unless required by applicable law or agreed to in writing, software
#  distributed under the License is distributed on an "AS IS" BASIS,
#  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#  See the License for the specific language governing permissions and
#  limitations under the License.
#

"""
Reusable HTML email templates and registry.
"""

# Invitation email template
INVITE_EMAIL_TMPL = """
Hi {{email}},
{{inviter}} has invited you to join their team (ID: {{tenant_id}}).
Click the link below to complete your registration:
{{invite_url}}
If you did not request this, please ignore this email.
"""

# Password reset code template
RESET_CODE_EMAIL_TMPL = """
Hello,
Your password reset code is: {{ code }}
This code will expire in {{ ttl_min }} minutes.
"""

# Downgrade warning email (daily, rate-limited)
DOWNGRADE_WARNING_EMAIL_TMPL = """
Hi {{ nickname }},

Your scheduled downgrade ({{ current_plan }} -> {{ target_plan }}) will take
effect in {{ remaining_days }} day(s) ({{ downgrade_date }}).  However, your
current resource usage exceeds the downgraded plan's quota limits:

- Storage: {{ current_storage }} used, {{ target_storage }} limit after downgrade
- Members: {{ current_members }} used, {{ target_members }} limit after downgrade
- Apps:   {{ current_apps }} used, {{ target_apps }} limit after downgrade

If you do not reduce usage to within limits before the downgrade date, the
scheduled downgrade will be automatically cancelled.  No additional charges
have been applied.
"""

DOWNGRADE_CANCELLED_EMAIL_TMPL = """
Hi {{ nickname }},

Your scheduled downgrade ({{ current_plan }} -> {{ target_plan }}) has been
automatically cancelled because your current resource usage exceeds the
downgraded plan's quota limits:

- Storage: {{ current_storage }} used, {{ target_storage }} limit after downgrade
- Members: {{ current_members }} used, {{ target_members }} limit after downgrade
- Apps:   {{ current_apps }} used, {{ target_apps }} limit after downgrade

No additional charges have been applied.  If you still wish to downgrade,
please reduce your usage to within the target plan's limits and resubmit the
downgrade request.

If you have any questions, please contact our support team.
"""

DOWNGRADE_EFFECTIVE_EXCEEDED_EMAIL_TMPL = """
Hi {{ nickname }},

Your downgrade ({{ current_plan }} -> {{ target_plan }}) has taken effect, but your
current resource usage exceeds the new plan's quota limits:

- Storage: {{ current_storage }} used, {{ target_storage }} current limit
- Members: {{ current_members }} used, {{ target_members }} current limit
- Apps:   {{ current_apps }} used, {{ target_apps }} current limit

You may encounter restrictions when uploading files, adding members, or
performing other operations that require additional quota.  Please contact
our support team for assistance.
"""

# Template registry
EMAIL_TEMPLATES = {
    "invite": INVITE_EMAIL_TMPL,
    "reset_code": RESET_CODE_EMAIL_TMPL,
    "downgrade_warning": DOWNGRADE_WARNING_EMAIL_TMPL,
    "downgrade_cancelled": DOWNGRADE_CANCELLED_EMAIL_TMPL,
    "downgrade_effective_exceeded": DOWNGRADE_EFFECTIVE_EXCEEDED_EMAIL_TMPL,
}
