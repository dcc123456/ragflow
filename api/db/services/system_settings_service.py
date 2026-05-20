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
from datetime import datetime
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
    def get_first_by_name(cls, name):
        return cls.model.select().where(cls.model.name.startswith(name)).first()

    @classmethod
    @DB.connection_context()
    def get_by_exact_name(cls, name: str):
        return cls.model.select().where(cls.model.name == name)

    @classmethod
    @DB.connection_context()
    def get_singleton_by_exact_name(cls, name: str):
        rows = list(
            cls.model.select()
            .where(cls.model.name == name)
            .order_by(cls.model.update_time.desc(), cls.model.create_time.desc())
        )
        if not rows:
            return None
        keeper = rows[0]
        duplicate_ids = [row.id for row in rows[1:]]
        if duplicate_ids:
            cls.model.delete().where(cls.model.id.in_(duplicate_ids)).execute()
        return keeper

    @classmethod
    @DB.connection_context()
    def get_by_source(cls, source: str):
        objs = cls.model.select().where(cls.model.source == source)
        return objs

    @classmethod
    @DB.connection_context()
    def insert(cls, **kwargs):
        """Insert a new record with automatic ID and timestamps.

        This method creates a new record with automatically generated ID and timestamp fields.
        It handles the creation of create_time, create_date, update_time, and update_date fields.

        Args:
            **kwargs: Record field values as keyword arguments.

        Returns:
            Model instance: The newly created record object.
        """
        import logging
        logging.info(f"about to insert {kwargs=}")
        timestamp = current_timestamp()
        cur_datetime = datetime_format(datetime.now())
        kwargs["create_time"] = timestamp
        kwargs["create_date"] = cur_datetime
        kwargs["update_time"] = timestamp
        kwargs["update_date"] = cur_datetime
        sample_obj = cls.model(**kwargs).save(force_insert=True)
        return sample_obj

    @classmethod
    @DB.connection_context()
    def update_by_name(cls, name, obj):
        obj["update_time"] = current_timestamp()
        obj["update_date"] = datetime_format(datetime.now())
        cls.model.update(obj).where(cls.model.name.startswith(name)).execute()
        return SystemSettings(**obj)

    @classmethod
    @DB.connection_context()
    def update_by_exact_name(cls, name: str, obj: dict):
        obj["update_time"] = current_timestamp()
        obj["update_date"] = datetime_format(datetime.now())
        cls.model.update(obj).where(cls.model.name == name).execute()
        return SystemSettings(**obj)

    @classmethod
    @DB.connection_context()
    def upsert_singleton_by_exact_name(
        cls,
        *,
        name: str,
        source: str,
        data_type: str,
        value: str,
    ):
        current = cls.get_singleton_by_exact_name(name)
        if current:
            return cls.update_by_exact_name(name, {"value": value, "source": source, "data_type": data_type})
        return cls.insert(name=name, source=source, data_type=data_type, value=value)

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
    def delete_by_exact_name(cls, name: str):
        return cls.model.delete().where(cls.model.name == name).execute()

    @classmethod
    @DB.connection_context()
    def delete_by_source_and_name(cls, source: str, name: str):
        return cls.model.delete().where((cls.model.source == source) & (cls.model.name.startswith(name))).execute()

    @classmethod
    @DB.connection_context()
    def get_oauth_config(cls):
        oauth_config = {}
        github_sso_config = cls.get_by_source("github|sso")
        if github_sso_config:
            setting_dict = {var.name.split(".")[-1]: load_value_from_string(var.value, var.data_type) for var in
                            github_sso_config}
            setting_dict.update({"type": "github"})
            if setting_dict.get("enabled"):
                oauth_config.update({"github": setting_dict})
        feishu_sso_config = cls.get_by_source("feishu|sso")
        if feishu_sso_config:
            setting_dict = {var.name.split(".")[-1]: load_value_from_string(var.value, var.data_type) for var in
                            feishu_sso_config}
            setting_dict.update({"type": "feishu"})
            if setting_dict.get("enabled"):
                oauth_config.update({"feishu": setting_dict})
        google_sso_config = cls.get_by_source("google|sso")
        if google_sso_config:
            setting_dict = {var.name.split(".")[-1]: load_value_from_string(var.value, var.data_type) for var in
                            google_sso_config}
            setting_dict.update({"type": "google"})
            if setting_dict.get("enabled"):
                oauth_config.update({"google": setting_dict})
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
            oauth_config.update(enabled_ldap_config)
        return oauth_config

    @classmethod
    def get_channel_oauth_config(cls, channel: str) -> dict:
        if channel.startswith("ldap"):
            source = "ldap|default" if channel == "ldap" else channel
        else:
            source = f"{channel}|sso"
        channel_oauth_configs = cls.get_by_source(source)
        if channel_oauth_configs:
            setting_dict = {
                var.name.split(".")[-1]: load_value_from_string(var.value, var.data_type) for var in channel_oauth_configs
            }
            oauth_type = "ldap" if channel.startswith("ldap") else channel
            setting_dict.update({"type": oauth_type})
            return setting_dict
        return {}

    @classmethod
    def get_smtp_config(cls):
        mail_config_rows = cls.get_by_name("mail")
        if not mail_config_rows:
            return {}
        mail_config = {
            f'mail_{row.name.split(".")[-1]}': load_value_from_string(row.value, row.data_type) for row in mail_config_rows
        }
        return mail_config
