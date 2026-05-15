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
import boto3
from botocore.exceptions import ClientError
from botocore.config import Config
import time
from io import BytesIO
from common.decorator import singleton
from common import settings


@singleton
class RAGFlowS3:
    def __init__(self):
        self.conn = None
        # Don't cache settings.S3 to avoid circular import issues
        # Instead, read it dynamically via properties
        # self.__open__()

    @property
    def s3_config(self):
        """Dynamically read S3 config to avoid circular import issues"""
        return settings.S3 or {}

    @property
    def access_key(self):
        return self.s3_config.get('access_key', None)

    @property
    def secret_key(self):
        return self.s3_config.get('secret_key', None)

    @property
    def session_token(self):
        return self.s3_config.get('session_token', None)

    @property
    def region_name(self):
        return self.s3_config.get('region_name', None)

    @property
    def endpoint_url(self):
        return self.s3_config.get('endpoint_url', None)

    @property
    def signature_version(self):
        return self.s3_config.get('signature_version', None)

    @property
    def addressing_style(self):
        return self.s3_config.get('addressing_style', None)

    @property
    def bucket(self):
        return self.s3_config.get('bucket', None)

    @property
    def prefix_path(self):
        return self.s3_config.get('prefix_path', None)

    def _ensure_connection(self):
        """Lazy connection initialization - only create when first needed"""
        if self.conn is None:
            self.__open__()

    @staticmethod
    def use_default_bucket(method):
        def wrapper(self, bucket, *args, **kwargs):
            # Ensure connection is established before accessing self.bucket
            self._ensure_connection()
            # If there is a default bucket, use the default bucket
            actual_bucket = self.bucket if self.bucket else bucket
            return method(self, actual_bucket, *args, **kwargs)

        return wrapper

    @staticmethod
    def use_prefix_path(method):
        def wrapper(self, bucket, fnm, *args, **kwargs):
            # Ensure connection is established before accessing self.prefix_path
            self._ensure_connection()
            # If the prefix path is set, use the prefix path.
            # The bucket passed from the upstream call is
            # used as the file prefix. This is especially useful when you're using the default bucket
            if self.prefix_path:
                fnm = f"{self.prefix_path}/{bucket}/{fnm}"
            return method(self, bucket, fnm, *args, **kwargs)

        return wrapper

    def __open__(self):
        try:
            if self.conn:
                self.__close__()
        except Exception:
            pass

        try:
            s3_params = {}
            config_kwargs = {}
            # if not set ak/sk, boto3 s3 client would try several ways to do the authentication
            # see doc: https://boto3.amazonaws.com/v1/documentation/api/latest/guide/credentials.html#configuring-credentials
            if self.access_key and self.secret_key:
                s3_params = {
                    'aws_access_key_id': self.access_key,
                    'aws_secret_access_key': self.secret_key,
                    'aws_session_token': self.session_token,
                }
            if self.region_name:
                s3_params['region_name'] = self.region_name
            if self.endpoint_url:
                s3_params['endpoint_url'] = self.endpoint_url

            # Configure signature_version and addressing_style through Config object
            if self.signature_version:
                config_kwargs['signature_version'] = self.signature_version
            if self.addressing_style:
                config_kwargs['s3'] = {'addressing_style': self.addressing_style}

            if config_kwargs:
                s3_params['config'] = Config(**config_kwargs)

            self.conn = [boto3.client('s3', **s3_params)]
        except Exception:
            logging.exception(f"Fail to connect at region {self.region_name} or endpoint {self.endpoint_url}")

    def __close__(self):
        del self.conn[0]
        self.conn = None

    def _actual_bucket(self, bucket):
        return self.bucket if self.bucket else bucket

    def _object_key(self, bucket, fnm):
        return f"{self.prefix_path}/{bucket}/{fnm}" if self.prefix_path else fnm

    @use_default_bucket
    def bucket_exists(self, bucket, *args, **kwargs):
        self._ensure_connection()
        try:
            logging.debug(f"head_bucket bucketname {bucket}")
            self.conn[0].head_bucket(Bucket=bucket)
            exists = True
        except ClientError:
            logging.exception(f"head_bucket error {bucket}")
            exists = False
        return exists

    def health(self):
        self._ensure_connection()
        bucket = self.bucket
        fnm = "txtxtxtxt1"
        fnm, binary = f"{self.prefix_path}/{fnm}" if self.prefix_path else fnm, b"_t@@@1"
        if not self.bucket_exists(bucket):
            self.conn[0].create_bucket(Bucket=bucket)
            logging.debug(f"create bucket {bucket} ********")

        self.conn[0].upload_fileobj(BytesIO(binary), bucket, fnm)
        return True

    def get_properties(self, bucket, key):
        return {}

    def list(self, bucket, dir, recursive=True):
        return []

    @use_prefix_path
    @use_default_bucket
    def put(self, bucket, fnm, binary, *args, **kwargs):
        self._ensure_connection()
        logging.debug(f"bucket name {bucket}; filename :{fnm}:")
        for _ in range(1):
            try:
                if not self.bucket_exists(bucket):
                    self.conn[0].create_bucket(Bucket=bucket)
                    logging.info(f"create bucket {bucket} ********")
                r = self.conn[0].upload_fileobj(BytesIO(binary), bucket, fnm)

                return r
            except Exception:
                logging.exception(f"Fail put {bucket}/{fnm}")
                self.__open__()
                time.sleep(1)

    @use_prefix_path
    @use_default_bucket
    def rm(self, bucket, fnm, *args, **kwargs):
        self._ensure_connection()
        try:
            self.conn[0].delete_object(Bucket=bucket, Key=fnm)
        except Exception:
            logging.exception(f"Fail rm {bucket}/{fnm}")

    @use_prefix_path
    @use_default_bucket
    def get(self, bucket, fnm, *args, **kwargs):
        self._ensure_connection()
        for _ in range(1):
            try:
                r = self.conn[0].get_object(Bucket=bucket, Key=fnm)
                object_data = r['Body'].read()
                return object_data
            except Exception:
                logging.exception(f"fail get {bucket}/{fnm}")
                self.__open__()
                time.sleep(1)
        return None

    @use_prefix_path
    @use_default_bucket
    def obj_exist(self, bucket, fnm, *args, **kwargs):
        self._ensure_connection()
        try:
            if self.conn[0].head_object(Bucket=bucket, Key=fnm):
                return True
        except ClientError as e:
            if e.response['Error']['Code'] == '404':
                return False
            else:
                raise

    def copy(self, src_bucket, src_path, dest_bucket, dest_path, *args, **kwargs):
        self._ensure_connection()
        try:
            actual_src_bucket = self._actual_bucket(src_bucket)
            actual_dest_bucket = self._actual_bucket(dest_bucket)
            actual_src_path = self._object_key(src_bucket, src_path)
            actual_dest_path = self._object_key(dest_bucket, dest_path)

            if not self.bucket_exists(actual_dest_bucket):
                self.conn[0].create_bucket(Bucket=actual_dest_bucket)

            self.conn[0].copy_object(
                Bucket=actual_dest_bucket,
                Key=actual_dest_path,
                CopySource={"Bucket": actual_src_bucket, "Key": actual_src_path},
            )
            return True
        except Exception:
            logging.exception(f"Fail to copy {src_bucket}/{src_path} -> {dest_bucket}/{dest_path}")
            return False

    def move(self, src_bucket, src_path, dest_bucket, dest_path, *args, **kwargs):
        self._ensure_connection()
        try:
            if self.copy(src_bucket, src_path, dest_bucket, dest_path, *args, **kwargs):
                actual_src_bucket = self._actual_bucket(src_bucket)
                actual_src_path = self._object_key(src_bucket, src_path)
                self.conn[0].delete_object(Bucket=actual_src_bucket, Key=actual_src_path)
                return True
            logging.error(f"Copy failed, move aborted: {src_bucket}/{src_path}")
            return False
        except Exception:
            logging.exception(f"Fail to move {src_bucket}/{src_path} -> {dest_bucket}/{dest_path}")
            return False

    def remove_bucket(self, bucket, *args, **kwargs):
        self._ensure_connection()
        try:
            actual_bucket = self._actual_bucket(bucket)
            if not self.bucket_exists(actual_bucket):
                return
            paginator = self.conn[0].get_paginator("list_objects_v2")
            prefix = f"{self.prefix_path}/{bucket}/" if self.prefix_path else ""
            paginate_kwargs = {"Bucket": actual_bucket}
            if prefix:
                paginate_kwargs["Prefix"] = prefix
            for page in paginator.paginate(**paginate_kwargs):
                for obj in page.get("Contents", []):
                    self.conn[0].delete_object(Bucket=actual_bucket, Key=obj["Key"])
            if not prefix:
                self.conn[0].delete_bucket(Bucket=actual_bucket)
        except Exception:
            logging.exception(f"Fail to remove bucket {bucket}")

    @use_prefix_path
    @use_default_bucket
    def get_presigned_url(self, bucket, fnm, expires, *args, **kwargs):
        self._ensure_connection()
        for _ in range(10):
            try:
                r = self.conn[0].generate_presigned_url('get_object',
                                                        Params={'Bucket': bucket,
                                                                'Key': fnm},
                                                        ExpiresIn=expires)

                return r
            except Exception:
                logging.exception(f"fail get url {bucket}/{fnm}")
                self.__open__()
                time.sleep(1)
        return None

    @use_default_bucket
    def rm_bucket(self, bucket, *args, **kwargs):
        self._ensure_connection()
        for conn in self.conn:
            try:
                if not conn.bucket_exists(bucket):
                    continue
                for o in conn.list_objects_v2(Bucket=bucket):
                    conn.delete_object(bucket, o.object_name)
                conn.delete_bucket(Bucket=bucket)
                return
            except Exception as e:
                logging.error(f"Fail rm {bucket}: " + str(e))
