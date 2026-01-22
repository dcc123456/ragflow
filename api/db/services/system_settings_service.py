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
import logging
from datetime import datetime
from common import settings
from common.time_utils import current_timestamp, datetime_format
from api.db.db_models import DB
from api.db.db_models import SystemSettings
from api.db.services.common_service import CommonService
from api.utils.system_settings_utils import load_value_from_string


class SystemSettingsService(CommonService):
    model = SystemSettings

    @classmethod
    @DB.connection_context()
    def get_by_name(cls, name):
        objs = cls.model.select().where(cls.model.name.startswith(name))
        return objs

    @classmethod
    @DB.connection_context()
    def get_by_source(cls, source: str):
        objs = cls.model.select().where(cls.model.source == source)
        return objs

    @classmethod
    @DB.connection_context()
    def update_by_name(cls, name, obj):
        obj["update_time"] = current_timestamp()
        obj["update_date"] = datetime_format(datetime.now())
        cls.model.update(obj).where(cls.model.name.startswith(name)).execute()
        return SystemSettings(**obj)

    @classmethod
    @DB.connection_context()
    def get_record_count(cls):
        count = cls.model.select().count()
        return count

    @classmethod
    @DB.connection_context()
    def delete_by_source(cls, source: str):
        return cls.model.delete().where(cls.model.source == source).execute()

    @classmethod
    @DB.connection_context()
    def delete_by_name(cls, name: str):
        return cls.model.delete().where(cls.model.name.startswith(name)).execute()

    @classmethod
    @DB.connection_context()
    def delete_by_source_and_name(cls, source: str, name: str):
        return cls.model.delete().where((cls.model.source == source) & (cls.model.name.startswith(name))).execute()

    @classmethod
    def refresh_oauth_config(cls):
        settings.OAUTH_CONFIG = {}
        github_sso_config = cls.get_by_source("github|sso")
        if github_sso_config:
            setting_dict = {var.name.split(".")[-1]: load_value_from_string(var.value, var.data_type) for var in
                            github_sso_config}
            setting_dict.update({"type": "github"})
            if setting_dict.get("enabled"):
                settings.OAUTH_CONFIG.update({"github": setting_dict})
                settings.GITHUB_OAUTH = setting_dict
                logging.info("Set GitHub OAuth config from system settings: {}".format({**setting_dict, "secret_key": "******"}))
        feishu_sso_config = cls.get_by_source("feishu|sso")
        if feishu_sso_config:
            setting_dict = {var.name.split(".")[-1]: load_value_from_string(var.value, var.data_type) for var in
                            feishu_sso_config}
            setting_dict.update({"type": "feishu"})
            if setting_dict.get("enabled"):
                settings.OAUTH_CONFIG.update({"feishu": setting_dict})
                settings.FEISHU_OAUTH = setting_dict
                logging.info("Set Feishu OAuth config from system settings: {}".format({**setting_dict, "app_secret": "******"}))
        google_sso_config = cls.get_by_source("google|sso")
        if google_sso_config:
            setting_dict = {var.name.split(".")[-1]: load_value_from_string(var.value, var.data_type) for var in
                            google_sso_config}
            setting_dict.update({"type": "google"})
            if setting_dict.get("enabled"):
                settings.OAUTH_CONFIG.update({"google": setting_dict})
                settings.GOOGLE_OAUTH = setting_dict
                logging.info("Set Google OAuth config from system settings: {}".format({**setting_dict, "client_secret": "******"}))
        ldap_configs = cls.get_by_name("ldap")
        if ldap_configs:
            ldap_config_mapping = {}
            for config in ldap_configs:
                channel_name = "ldap" if config.source == "ldap|default" else config.source
                if ldap_config_mapping.get(channel_name):
                    ldap_config_mapping[channel_name].update(
                        {config.name.split(".")[-1]: load_value_from_string(config.value, config.data_type)})
                else:
                    ldap_config_mapping[channel_name] = {
                        config.name.split(".")[-1]: load_value_from_string(config.value, config.data_type)}
            enabled_ldap_config = {k: v for k, v in ldap_config_mapping.items() if v.get("enabled")}
            for v in enabled_ldap_config.values():
                v.update({"type": "ldap"})
            settings.OAUTH_CONFIG.update(enabled_ldap_config)
            logging.info("Set LDAP OAuth config from system settings: {}".format({k: {**v, "password": "******"} for k, v in enabled_ldap_config.items()}))
            if enabled_ldap_config.get("ldap"):
                settings.LDAP_OAUTH = ldap_config_mapping["ldap"]

    @classmethod
    def refresh_smtp_config(cls):
        mail_config_rows = SystemSettingsService.get_by_name("mail")
        if not mail_config_rows:
            return
        mail_config = {row.name.split(".")[-1]: load_value_from_string(row.value, row.data_type) for row in
                       mail_config_rows}
        logging.info("Get mail config from system settings: {}".format({**mail_config, "mail_password": "******"}))
        settings.SMTP_CONF = {f"mail_{k}": v for k, v in mail_config.items()}
        settings.MAIL_USE_SSL = settings.SMTP_CONF.get("mail_use_ssl", True)
        settings.MAIL_USE_TLS = settings.SMTP_CONF.get("mail_use_tls", False)
        settings.MAIL_USERNAME = settings.SMTP_CONF.get("mail_username", "")
        settings.MAIL_PASSWORD = settings.SMTP_CONF.get("mail_password", "")
        mail_default_sender = settings.SMTP_CONF.get("mail_default_sender", [])
        if mail_default_sender and len(mail_default_sender) >= 2:
            settings.MAIL_DEFAULT_SENDER = (mail_default_sender[0], mail_default_sender[1])
        elif mail_default_sender and isinstance(mail_default_sender, str):
            settings.MAIL_DEFAULT_SENDER = mail_default_sender
        settings.MAIL_FRONTEND_URL = settings.SMTP_CONF.get("mail_frontend_url", "")
        logging.info("Set SMTP config from system settings: {}".format({**settings.SMTP_CONF, "mail_password": "******"}))
