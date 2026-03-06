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

import logging
import os
import time
from common.decorator import singleton
from azure.identity import ClientSecretCredential, AzureAuthorityHosts
from azure.storage.filedatalake import FileSystemClient
from common import settings


@singleton
class RAGFlowAzureSpnBlob:
    def __init__(self):
        self.conn = None
        # Don't open connection in __init__ - wait for first use
        # This avoids circular import issue where AZURE config is empty during import

    @property
    def azure_config(self):
        """Dynamically read AZURE config to avoid circular import issues"""
        return settings.AZURE or {}

    @property
    def account_url(self):
        return os.getenv('ACCOUNT_URL', self.azure_config.get("account_url"))

    @property
    def client_id(self):
        return os.getenv('CLIENT_ID', self.azure_config.get("client_id"))

    @property
    def secret(self):
        return os.getenv('SECRET', self.azure_config.get("secret"))

    @property
    def tenant_id(self):
        return os.getenv('TENANT_ID', self.azure_config.get("tenant_id"))

    @property
    def container_name(self):
        return os.getenv('CONTAINER_NAME', self.azure_config.get("container_name"))

    def _ensure_connection(self):
        """Lazy connection initialization - only create when first needed"""
        if self.conn is None:
            self.__open__()

    def __open__(self):
        try:
            if self.conn:
                self.__close__()
        except Exception:
            pass

        try:
            # Get config values
            account_url = self.account_url
            client_id = self.client_id
            secret = self.secret
            tenant_id = self.tenant_id
            container_name = self.container_name

            # Check if all required config values are present
            if not account_url or not client_id or not secret or not tenant_id or not container_name:
                logging.error("Missing required Azure SPN configuration")
                return

            credentials = ClientSecretCredential(tenant_id=tenant_id, client_id=client_id,
                                                 client_secret=secret, authority=AzureAuthorityHosts.AZURE_CHINA)
            self.conn = FileSystemClient(account_url=account_url, file_system_name=container_name,
                                         credential=credentials)
        except Exception:
            logging.exception("Fail to connect to Azure")

    def __close__(self):
        del self.conn
        self.conn = None

    def health(self):
        self._ensure_connection()
        _bucket, fnm, binary = "txtxtxtxt1", "txtxtxtxt1", b"_t@@@1"
        f = self.conn.create_file(fnm)
        f.append_data(binary, offset=0, length=len(binary))
        return f.flush_data(len(binary))

    def put(self, bucket, fnm, binary):
        self._ensure_connection()
        for _ in range(3):
            try:
                f = self.conn.create_file(fnm)
                f.append_data(binary, offset=0, length=len(binary))
                return f.flush_data(len(binary))
            except Exception:
                logging.exception(f"Fail put {bucket}/{fnm}")
                self.__open__()
                time.sleep(1)
                return None
        return None

    def rm(self, bucket, fnm):
        self._ensure_connection()
        try:
            self.conn.delete_file(fnm)
        except Exception:
            logging.exception(f"Fail rm {bucket}/{fnm}")

    def get(self, bucket, fnm):
        self._ensure_connection()
        for _ in range(1):
            try:
                client = self.conn.get_file_client(fnm)
                r = client.download_file()
                return r.read()
            except Exception:
                logging.exception(f"fail get {bucket}/{fnm}")
                self.__open__()
                time.sleep(1)
        return None

    def obj_exist(self, bucket, fnm):
        self._ensure_connection()
        try:
            client = self.conn.get_file_client(fnm)
            return client.exists()
        except Exception:
            logging.exception(f"Fail put {bucket}/{fnm}")
        return False

    def get_presigned_url(self, bucket, fnm, expires):
        self._ensure_connection()
        for _ in range(10):
            try:
                return self.conn.get_presigned_url("GET", bucket, fnm, expires)
            except Exception:
                logging.exception(f"fail get {bucket}/{fnm}")
                self.__open__()
                time.sleep(1)
        return None
