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
import time
import datetime
from io import BytesIO
from google.cloud import storage
from google.api_core.exceptions import NotFound
from common.decorator import singleton
from common import settings


@singleton
class RAGFlowGCS:
    def __init__(self):
        self.client = None
        self._bucket_name = None
        # Don't open connection in __init__ - wait for first use
        # This avoids circular import issue where GCS config is empty during import

    @property
    def gcs_config(self):
        """Dynamically read GCS config to avoid circular import issues"""
        cfg = settings.GCS
        # GCS can be a list in older configs, handle both cases
        if isinstance(cfg, list) and len(cfg) > 0:
            return cfg[0] if isinstance(cfg[0], dict) else {}
        return cfg if isinstance(cfg, dict) else {}

    @property
    def bucket_name(self):
        # Prefer explicitly set _bucket_name, fallback to config
        return self._bucket_name if self._bucket_name else self.gcs_config.get("bucket", None)

    @bucket_name.setter
    def bucket_name(self, value):
        self._bucket_name = value

    def _ensure_connection(self):
        """Lazy connection initialization - only create when first needed"""
        if self.client is None:
            self.__open__()

    def __open__(self):
        try:
            if self.client:
                self.client = None
        except Exception:
            pass

        try:
            self.client = storage.Client()
            # bucket_name is a property, not settable - just rely on gcs_config.get("bucket") in the property
        except Exception:
            logging.exception("Fail to connect to GCS")

    def _get_blob_path(self, folder, filename):
        """Helper to construct the path: folder/filename"""
        if not folder:
            return filename
        return f"{folder}/{filename}"

    def health(self):
        self._ensure_connection()
        folder, fnm, binary = "ragflow-health", "health_check", b"_t@@@1"
        try:
            bucket_obj = self.client.bucket(self.bucket_name)
            if not bucket_obj.exists():
                logging.error(f"Health check failed: Main bucket '{self.bucket_name}' does not exist.")
                return False

            blob_path = self._get_blob_path(folder, fnm)
            blob = bucket_obj.blob(blob_path)
            blob.upload_from_file(BytesIO(binary), content_type='application/octet-stream')
            return True
        except Exception as e:
            logging.exception(f"Health check failed: {e}")
            return False

    def put(self, bucket, fnm, binary, tenant_id=None):
        # RENAMED PARAMETER: bucket_name -> bucket (to match interface)
        self._ensure_connection()
        for _ in range(3):
            try:
                bucket_obj = self.client.bucket(self.bucket_name)
                blob_path = self._get_blob_path(bucket, fnm)
                blob = bucket_obj.blob(blob_path)
                blob.upload_from_file(BytesIO(binary), content_type='application/octet-stream')
                return True
            except NotFound:
                logging.error(f"Fail to put: Main bucket {self.bucket_name} does not exist.")
                return False
            except Exception:
                logging.exception(f"Fail to put {bucket}/{fnm}:")
                self.__open__()
                time.sleep(1)
        return False

    def rm(self, bucket, fnm, tenant_id=None):
        # RENAMED PARAMETER: bucket_name -> bucket
        self._ensure_connection()
        try:
            bucket_obj = self.client.bucket(self.bucket_name)
            blob_path = self._get_blob_path(bucket, fnm)
            blob = bucket_obj.blob(blob_path)
            blob.delete()
        except NotFound:
            pass
        except Exception:
            logging.exception(f"Fail to remove {bucket}/{fnm}:")

    def get(self, bucket, filename, tenant_id=None):
        # RENAMED PARAMETER: bucket_name -> bucket
        self._ensure_connection()
        for _ in range(1):
            try:
                bucket_obj = self.client.bucket(self.bucket_name)
                blob_path = self._get_blob_path(bucket, filename)
                blob = bucket_obj.blob(blob_path)
                return blob.download_as_bytes()
            except NotFound:
                logging.warning(f"File not found {bucket}/{filename} in {self.bucket_name}")
                return None
            except Exception:
                logging.exception(f"Fail to get {bucket}/{filename}:")
                self.__open__()
                time.sleep(1)
        return None

    def obj_exist(self, bucket, filename, tenant_id=None):
        # RENAMED PARAMETER: bucket_name -> bucket
        self._ensure_connection()
        try:
            bucket_obj = self.client.bucket(self.bucket_name)
            blob_path = self._get_blob_path(bucket, filename)
            blob = bucket_obj.blob(blob_path)
            return blob.exists()
        except Exception:
            logging.exception(f"obj_exist {bucket}/{filename} got exception")
            return False

    def bucket_exists(self, bucket):
        # RENAMED PARAMETER: bucket_name -> bucket
        self._ensure_connection()
        try:
            bucket_obj = self.client.bucket(self.bucket_name)
            return bucket_obj.exists()
        except Exception:
            logging.exception(f"bucket_exist check for {self.bucket_name} got exception")
            return False

    def get_presigned_url(self, bucket, fnm, expires, tenant_id=None):
        # RENAMED PARAMETER: bucket_name -> bucket
        self._ensure_connection()
        for _ in range(10):
            try:
                bucket_obj = self.client.bucket(self.bucket_name)
                blob_path = self._get_blob_path(bucket, fnm)
                blob = bucket_obj.blob(blob_path)
                expiration = expires
                if isinstance(expires, int):
                    expiration = datetime.timedelta(seconds=expires)
                url = blob.generate_signed_url(
                    version="v4",
                    expiration=expiration,
                    method="GET"
                )
                return url
            except Exception:
                logging.exception(f"Fail to get_presigned {bucket}/{fnm}:")
                self.__open__()
                time.sleep(1)
        return None

    def remove_bucket(self, bucket):
        # RENAMED PARAMETER: bucket_name -> bucket
        self._ensure_connection()
        try:
            bucket_obj = self.client.bucket(self.bucket_name)
            prefix = f"{bucket}/"
            blobs = list(self.client.list_blobs(self.bucket_name, prefix=prefix))
            if blobs:
                bucket_obj.delete_blobs(blobs)
        except Exception:
            logging.exception(f"Fail to remove virtual bucket (folder) {bucket}")

    def rm_bucket(self, bucket):
        """Remove all objects with prefix 'bucket/' from the shared physical bucket.

        In the shared-bucket architecture, each kb_id maps to a path prefix.
        This method only removes objects under that prefix, NOT the physical bucket.
        """
        self.remove_bucket(bucket)

    def copy(self, src_bucket, src_path, dest_bucket, dest_path, tenant_id=None):
        # RENAMED PARAMETERS to match original interface
        self._ensure_connection()
        try:
            bucket_obj = self.client.bucket(self.bucket_name)
            src_blob = bucket_obj.blob(self._get_blob_path(src_bucket, src_path))
            dest_blob = bucket_obj.blob(self._get_blob_path(dest_bucket, dest_path))
            dest_blob.rewrite(src_blob)
            return True
        except Exception:
            logging.exception(f"Fail to copy {src_bucket}/{src_path} -> {dest_bucket}/{dest_path}")
            return False

    def move(self, src_bucket, src_path, dest_bucket, dest_path):
        try:
            if self.copy(src_bucket, src_path, dest_bucket, dest_path):
                self.rm(src_bucket, src_path)
                return True
            else:
                logging.error(f"Copy failed, move aborted: {src_bucket}/{src_path}")
                return False
        except Exception:
            logging.exception(f"Fail to move {src_bucket}/{src_path} -> {dest_bucket}/{dest_path}")
            return False
