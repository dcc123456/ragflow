#
#  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
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
import aiosmtplib
from email.mime.text import MIMEText
from email.header import Header
from quart import render_template_string
from api.utils.email_templates import EMAIL_TEMPLATES
from api.db.services.system_settings_service import SystemSettingsService

async def send_email_html(to_email: str, subject: str, template_key: str, **context):

    body = await render_template_string(EMAIL_TEMPLATES.get(template_key), **context)
    msg = MIMEText(body, "plain", "utf-8")
    msg["Subject"] = Header(subject, "utf-8")
    mail_config = SystemSettingsService.get_smtp_config()
    if not mail_config or not mail_config.get("mail_server"):
        raise Exception("SMTP config is not set")
    msg["From"] = mail_config['mail_default_sender']
    msg["To"] = to_email

    smtp = aiosmtplib.SMTP(
        hostname=mail_config["mail_server"],
        port=mail_config["mail_port"],
        start_tls=mail_config["mail_use_tls"],  # tls usually means starttls at port 587
        use_tls=mail_config["mail_use_ssl"],  # ssl usually means direct tls at port 465
        timeout=10,
    )

    await smtp.connect()
    await smtp.login(mail_config["mail_username"], mail_config["mail_password"])
    await smtp.send_message(msg)
    await smtp.quit()


async def send_invite_email(to_email, invite_url, tenant_id, inviter):
    # Reuse the generic HTML sender with 'invite' template
    await send_email_html(
        to_email=to_email,
        subject="RAGFlow Invitation",
        template_key="invite",
        email=to_email,
        invite_url=invite_url,
        tenant_id=tenant_id,
        inviter=inviter,
    )
