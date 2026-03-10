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
import hashlib
import logging
import ssl
import time
from minio import Minio
from minio.commonconfig import CopySource
from io import BytesIO
import urllib3
from common.decorator import singleton
from common import settings


def _build_minio_http_client():
    """
    Build an optional urllib3 HTTP client for MinIO when using SSL/TLS.
    Respects MINIO.verify (default True) to allow self-signed certificates
    when set to False.
    """
    verify = settings.MINIO.get("verify", True)
    if verify is True or verify == "true" or verify == "1":
        return None
    return urllib3.PoolManager(cert_reqs=ssl.CERT_NONE)


@singleton
class RAGFlowMinio:
    def __init__(self):
        self.conn = []
        # Don't open connection in __init__ - wait for first use
        # This avoids circular import issue where MINIO config is empty during import

    @property
    def minio_config(self):
        """Dynamically read MINIO config to avoid circular import issues"""
        return settings.MINIO or []

    def _ensure_connection(self):
        """Lazy connection initialization - only create when first needed"""
        if not self.conn:
            self.__open__()

    def __open__(self):
        try:
            if self.conn:
                self.__close__()
        except Exception:
            pass

        try:
            secure = settings.MINIO.get("secure", False)
            if isinstance(secure, str):
                secure = secure.lower() in ("true", "1", "yes")
            http_client = _build_minio_http_client()
            self.conn = Minio(
                settings.MINIO["host"],
                access_key=settings.MINIO["user"],
                secret_key=settings.MINIO["password"],
                secure=secure,
                http_client=http_client,
            )
        except Exception:
            logging.exception(
                "Fail to connect %s " % settings.MINIO["host"])

    def __close__(self):
        if self.conn:
            for c in self.conn:
                del c
        self.conn = []

    def health(self):
        self._ensure_connection()
        bucket, fnm, binary = "txtxtxtxt1", "txtxtxtxt1", b"_t@@@1"
        if not self.conn[0].bucket_exists(bucket):
            self.conn[0].make_bucket(bucket)
        r = self.conn[0].put_object(bucket, fnm,
                                 BytesIO(binary),
                                 len(binary)
                                 )
        return r

    def user_gateway(self, tenant_id):
        """Get connection index for tenant using hash-based distribution"""
        hash_obj = hashlib.sha256(tenant_id.encode("utf-8"))
        config_len = len(self.minio_config)
        return (int(hash_obj.hexdigest(), 16) % config_len) if config_len > 0 else 0

    def put(self, bucket, fnm, binary, tenant_id=None):
        self._ensure_connection()
        for _ in range(3):
            i = self.user_gateway(tenant_id)
            try:
                if not self.conn[i].bucket_exists(bucket):
                    self.conn[i].make_bucket(bucket)

                r = self.conn[i].put_object(bucket, fnm,
                                         BytesIO(binary),
                                         len(binary)
                                         )
                return r
            except Exception as e:
                logging.error(f"Fail put {bucket}/{fnm}: " + str(e))
                self.__open__()
                time.sleep(1)

    def rm(self, bucket, fnm, tenant_id=None):
        self._ensure_connection()
        try:
            i = self.user_gateway(tenant_id)
            self.conn[i].remove_object(bucket, fnm)
        except Exception as e:
            logging.error(f"Fail rm {bucket}/{fnm}: " + str(e))

    def rm_bucket(self, bucket):
        self._ensure_connection()
        for conn in self.conn:
            try:
                if not conn.bucket_exists(bucket):
                    continue
                for o in conn.list_objects(bucket, recursive=True):
                    conn.remove_object(bucket, o.object_name)
                conn.remove_bucket(bucket)
                return
            except Exception as e:
                logging.error(f"Fail rm {bucket}: " + str(e))

    def get(self, bucket, filename, tenant_id=None):
        self._ensure_connection()
        for _ in range(1):
            i = self.user_gateway(tenant_id)
            try:
                r = self.conn[i].get_object(bucket, filename)
                logging.info(f"Successfully get {bucket}/{filename}({i})")
                return r.read()
            except Exception as e:
                logging.error(f"fail get {bucket}/{filename}({i}): " + str(e))
                self.__open__()
                time.sleep(1)
            if i == 0:
                raise Exception("""File not found.""")
        return None

    def obj_exist(self, bucket, filename, tenant_id):
        self._ensure_connection()
        try:
            i = self.user_gateway(tenant_id)
            if self.conn[i].stat_object(bucket, filename):
                return True
            return False
        except Exception as e:
            logging.error(f"Fail exist {bucket}/{filename}: " + str(e))
        return False

    def get_presigned_url(self, bucket, fnm, expires, tenant_id=None):
        self._ensure_connection()
        for _ in range(3):
            try:
                i = self.user_gateway(tenant_id)
                return self.conn[i].get_presigned_url("GET", bucket, fnm, expires)
            except Exception as e:
                logging.error(f"fail get {bucket}/{fnm}: " + str(e))
                self.__open__()
                time.sleep(1)
        return

    def remove_bucket(self, bucket, tenant_id=None):
        self._ensure_connection()
        try:
            i = self.user_gateway(tenant_id)
            if self.conn[i].bucket_exists(bucket):
                objects_to_delete = self.conn[i].list_objects(bucket, recursive=True)
                for obj in objects_to_delete:
                    self.conn[i].remove_object(bucket, obj.object_name)
                self.conn[i].remove_bucket(bucket)
        except Exception:
            logging.exception(f"Fail to remove bucket {bucket}")

    def copy(self, src_bucket, src_path, dest_bucket, dest_path, tenant_id=None):
        self._ensure_connection()
        try:
            i = self.user_gateway(tenant_id)
            if not self.conn[i].bucket_exists(dest_bucket):
                self.conn[i].make_bucket(dest_bucket)

            try:
                self.conn[i].stat_object(src_bucket, src_path)
            except Exception as e:
                logging.exception(f"Source object not found: {src_bucket}/{src_path}, {e}")
                return False

            self.conn[i].copy_object(
                dest_bucket,
                dest_path,
                CopySource(src_bucket, src_path),
            )
            return True

        except Exception:
            logging.exception(f"Fail to copy {src_bucket}/{src_path} -> {dest_bucket}/{dest_path}")
            return False

    def move(self, src_bucket, src_path, dest_bucket, dest_path, tenant_id=None):
        self._ensure_connection()
        try:
            if self.copy(src_bucket, src_path, dest_bucket, dest_path, tenant_id):
                self.rm(src_bucket, src_path, tenant_id)
                return True
            else:
                logging.error(f"Copy failed, move aborted: {src_bucket}/{src_path}")
                return False
        except Exception:
            logging.exception(f"Fail to move {src_bucket}/{src_path} -> {dest_bucket}/{dest_path}")
            return False
