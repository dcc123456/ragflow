#
#  Copyright 2024 The InfiniFlow Authors. All Rights Reserved.
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
import contextvars
import hashlib
import inspect
import logging
import operator
import os
import sys
import time
import typing
from datetime import datetime, timezone
from enum import Enum
from functools import wraps

from quart_auth import AuthUser
from itsdangerous.url_safe import URLSafeTimedSerializer as Serializer
from peewee import (
    fn,
    Check,
    InterfaceError,
    OperationalError,
    ProgrammingError,
    BigIntegerField,
    BooleanField,
    CharField,
    CompositeKey,
    DateTimeField,
    Field,
    FloatField,
    IntegerField,
    Metadata,
    Model,
    TextField,
    PrimaryKeyField,
)
from playhouse.migrate import MySQLMigrator, PostgresqlMigrator, migrate
from playhouse.pool import PooledMySQLDatabase, PooledPostgresqlDatabase

from api import utils
from api.db import (
    PaymentStatus,
    ResourceType,
    SerializedType,
    TeamRole,
    UsageTraceStatus,
    PermissionValue,
    VALID_PERMISSION_ACTION_TYPES,
    VALID_PERMISSION_TARGET_TYPES,
    VALID_RESOURCE_TYPES,
)
from api.utils.json_encode import json_dumps, json_loads
from api.utils.configs import deserialize_b64, serialize_b64

from common.time_utils import current_timestamp, timestamp_to_date, date_string_to_timestamp
from common.decorator import singleton
from common.constants import ParserType
from common import settings


CONTINUOUS_FIELD_TYPE = {IntegerField, FloatField, DateTimeField}
AUTO_DATE_TIMESTAMP_FIELD_PREFIX = {"create", "start", "end", "update", "read_access", "write_access"}


class TextFieldType(Enum):
    MYSQL = "LONGTEXT"
    OCEANBASE = "LONGTEXT"
    POSTGRES = "TEXT"


class LongTextField(TextField):
    field_type = TextFieldType[settings.DATABASE_TYPE.upper()].value


class JSONField(LongTextField):
    default_value = {}

    def __init__(self, object_hook=None, object_pairs_hook=None, **kwargs):
        self._object_hook = object_hook
        self._object_pairs_hook = object_pairs_hook
        super().__init__(**kwargs)

    def db_value(self, value):
        if value is None:
            value = self.default_value
        return json_dumps(value)

    def python_value(self, value):
        if not value:
            return self.default_value
        return json_loads(value, object_hook=self._object_hook, object_pairs_hook=self._object_pairs_hook)


class ListField(JSONField):
    default_value = []


class SerializedField(LongTextField):
    def __init__(self, serialized_type=SerializedType.PICKLE, object_hook=None, object_pairs_hook=None, **kwargs):
        self._serialized_type = serialized_type
        self._object_hook = object_hook
        self._object_pairs_hook = object_pairs_hook
        super().__init__(**kwargs)

    def db_value(self, value):
        if self._serialized_type == SerializedType.PICKLE:
            return serialize_b64(value, to_str=True)
        elif self._serialized_type == SerializedType.JSON:
            if value is None:
                return None
            return json_dumps(value, with_type=True)
        else:
            raise ValueError(f"the serialized type {self._serialized_type} is not supported")

    def python_value(self, value):
        if self._serialized_type == SerializedType.PICKLE:
            return deserialize_b64(value)
        elif self._serialized_type == SerializedType.JSON:
            if value is None:
                return {}
            return json_loads(value, object_hook=self._object_hook, object_pairs_hook=self._object_pairs_hook)
        else:
            raise ValueError(f"the serialized type {self._serialized_type} is not supported")


def is_continuous_field(cls: typing.Type) -> bool:
    if cls in CONTINUOUS_FIELD_TYPE:
        return True
    for p in cls.__bases__:
        if p in CONTINUOUS_FIELD_TYPE:
            return True
        elif p is not Field and p is not object:
            if is_continuous_field(p):
                return True
    else:
        return False


def auto_date_timestamp_field():
    return {f"{f}_time" for f in AUTO_DATE_TIMESTAMP_FIELD_PREFIX}


def auto_date_timestamp_db_field():
    return {f"f_{f}_time" for f in AUTO_DATE_TIMESTAMP_FIELD_PREFIX}


def remove_field_name_prefix(field_name):
    return field_name[2:] if field_name.startswith("f_") else field_name


class BaseModel(Model):
    create_time = BigIntegerField(null=True, index=True)
    create_date = DateTimeField(null=True, index=True)
    update_time = BigIntegerField(null=True, index=True)
    update_date = DateTimeField(null=True, index=True)

    def to_json(self):
        # This function is obsolete
        return self.to_dict()

    def to_dict(self):
        return self.__dict__["__data__"]

    def to_human_model_dict(self, only_primary_with: list = None):
        model_dict = self.__dict__["__data__"]

        if not only_primary_with:
            return {remove_field_name_prefix(k): v for k, v in model_dict.items()}

        human_model_dict = {}
        for k in self._meta.primary_key.field_names:
            human_model_dict[remove_field_name_prefix(k)] = model_dict[k]
        for k in only_primary_with:
            human_model_dict[k] = model_dict[f"f_{k}"]
        return human_model_dict

    @property
    def meta(self) -> Metadata:
        return self._meta

    @classmethod
    def get_primary_keys_name(cls):
        return cls._meta.primary_key.field_names if isinstance(cls._meta.primary_key, CompositeKey) else [cls._meta.primary_key.name]

    @classmethod
    def getter_by(cls, attr):
        return operator.attrgetter(attr)(cls)

    @classmethod
    def query(cls, reverse=None, order_by=None, **kwargs):
        filters = []
        for f_n, f_v in kwargs.items():
            attr_name = "%s" % f_n
            if not hasattr(cls, attr_name) or f_v is None:
                continue
            if type(f_v) in {list, set}:
                f_v = list(f_v)
                if is_continuous_field(type(getattr(cls, attr_name))):
                    if len(f_v) == 2:
                        for i, v in enumerate(f_v):
                            if isinstance(v, str) and f_n in auto_date_timestamp_field():
                                # time type: %Y-%m-%d %H:%M:%S
                                f_v[i] = date_string_to_timestamp(v)
                        lt_value = f_v[0]
                        gt_value = f_v[1]
                        if lt_value is not None and gt_value is not None:
                            filters.append(cls.getter_by(attr_name).between(lt_value, gt_value))
                        elif lt_value is not None:
                            filters.append(operator.attrgetter(attr_name)(cls) >= lt_value)
                        elif gt_value is not None:
                            filters.append(operator.attrgetter(attr_name)(cls) <= gt_value)
                else:
                    filters.append(operator.attrgetter(attr_name)(cls) << f_v)
            else:
                filters.append(operator.attrgetter(attr_name)(cls) == f_v)
        if filters:
            query_records = cls.select().where(*filters)
            if reverse is not None:
                if not order_by or not hasattr(cls, f"{order_by}"):
                    order_by = "create_time"
                if reverse is True:
                    query_records = query_records.order_by(cls.getter_by(f"{order_by}").desc())
                elif reverse is False:
                    query_records = query_records.order_by(cls.getter_by(f"{order_by}").asc())
            return [query_record for query_record in query_records]
        else:
            return []

    @classmethod
    def insert(cls, __data=None, **insert):
        if isinstance(__data, dict) and __data:
            __data[cls._meta.combined["create_time"]] = current_timestamp()
        if insert:
            insert["create_time"] = current_timestamp()

        return super().insert(__data, **insert)

    # update and insert will call this method
    @classmethod
    def _normalize_data(cls, data, kwargs):
        normalized = super()._normalize_data(data, kwargs)
        if not normalized:
            return {}

        normalized[cls._meta.combined["update_time"]] = current_timestamp()

        for f_n in AUTO_DATE_TIMESTAMP_FIELD_PREFIX:
            if {f"{f_n}_time", f"{f_n}_date"}.issubset(cls._meta.combined.keys()) and cls._meta.combined[f"{f_n}_time"] in normalized and normalized[cls._meta.combined[f"{f_n}_time"]] is not None:
                normalized[cls._meta.combined[f"{f_n}_date"]] = timestamp_to_date(normalized[cls._meta.combined[f"{f_n}_time"]])

        return normalized


class JsonSerializedField(SerializedField):
    def __init__(self, object_hook=utils.from_dict_hook, object_pairs_hook=None, **kwargs):
        super(JsonSerializedField, self).__init__(serialized_type=SerializedType.JSON, object_hook=object_hook, object_pairs_hook=object_pairs_hook, **kwargs)


class _ContextVarConnectionState:
    """
    asyncio-compatible replacement for peewee's _ConnectionLocal (threading.local).

    In Quart/asyncio applications, all coroutines run in the same OS thread.
    peewee's default _ConnectionLocal uses threading.local, so all coroutines
    inadvertently share one connection state, causing:
      - Connection leaks (coroutine A's conn silently shared with coroutine B)
      - Double returns (both coroutines think they own the connection and return it)

    This class uses four independent contextvars.ContextVar instances (one per
    field) instead of a single ContextVar holding a mutable dict.

    Key design invariant: every setter calls _cv_X.set(val) which creates an
    *isolated* binding in the *current* asyncio-task context (or thread context).
    Inherited contexts are never mutated because ContextVar.set() only affects
    the current context's copy of the binding.  Using a single shared mutable
    dict (the previous approach) broke this invariant because dict mutations
    bypassed ContextVar's copy-on-write isolation and were visible to all
    sibling tasks that had inherited the same dict object.

    Attributes mirror _ConnectionState exactly:
      closed       (bool)   - whether the connection is closed
      conn         (object) - the active database connection
      ctx          (list)   - query context stack
      transactions (list)   - active transaction stack
    """

    def __init__(self):
        # One ContextVar per field.  Scalar fields use immutable defaults;
        # list fields default to None and create a fresh list on first access
        # (see property getters below).  Each _cv_X.set() call only affects
        # the current asyncio-task (or thread) context — never a sibling's.
        object.__setattr__(self, '_cv_closed',       contextvars.ContextVar('peewee_closed',       default=True))
        object.__setattr__(self, '_cv_conn',         contextvars.ContextVar('peewee_conn',         default=None))
        object.__setattr__(self, '_cv_ctx',          contextvars.ContextVar('peewee_ctx',          default=None))
        object.__setattr__(self, '_cv_transactions', contextvars.ContextVar('peewee_transactions', default=None))

    def reset(self):
        """Restore closed/empty state in the current context."""
        self._cv_closed.set(True)
        self._cv_conn.set(None)
        self._cv_ctx.set([])
        self._cv_transactions.set([])

    def set_connection(self, conn):
        """Called by peewee when a connection is checked out from the pool."""
        self._cv_closed.set(False)
        self._cv_conn.set(conn)
        self._cv_ctx.set([])
        self._cv_transactions.set([])

    # ------------------------------------------------------------------ #
    # closed                                                               #
    # ------------------------------------------------------------------ #
    @property
    def closed(self):
        return self._cv_closed.get()

    @closed.setter
    def closed(self, val):
        # set() creates/replaces the binding only in the current context —
        # sibling asyncio tasks that inherited the previous value are unaffected.
        self._cv_closed.set(val)

    # ------------------------------------------------------------------ #
    # conn                                                                 #
    # ------------------------------------------------------------------ #
    @property
    def conn(self):
        return self._cv_conn.get()

    @conn.setter
    def conn(self, val):
        self._cv_conn.set(val)

    # ------------------------------------------------------------------ #
    # ctx  (execution-context stack — peewee appends/pops in place)       #
    # ------------------------------------------------------------------ #
    @property
    def ctx(self):
        v = self._cv_ctx.get()
        if v is None:
            # First access in this context: allocate a fresh list that belongs
            # exclusively to this context (set() makes it context-local).
            v = []
            self._cv_ctx.set(v)
        return v

    @ctx.setter
    def ctx(self, val):
        self._cv_ctx.set(val)

    # ------------------------------------------------------------------ #
    # transactions                                                         #
    # ------------------------------------------------------------------ #
    @property
    def transactions(self):
        v = self._cv_transactions.get()
        if v is None:
            v = []
            self._cv_transactions.set(v)
        return v

    @transactions.setter
    def transactions(self, val):
        self._cv_transactions.set(val)


class RetryingPooledMySQLDatabase(PooledMySQLDatabase):
    def __init__(self, *args, **kwargs):
        self.max_retries = kwargs.pop("max_retries", 5)
        self.retry_delay = kwargs.pop("retry_delay", 1)
        # Force max_connections=100 and stale_timeout=30
        if 'max_connections' not in kwargs:
            kwargs['max_connections'] = 100
        if 'stale_timeout' not in kwargs:
            kwargs['stale_timeout'] = 30
        super().__init__(*args, **kwargs)
        # Replace threading.local with contextvars so each asyncio coroutine
        # running in the same OS thread gets its own isolated connection state.
        # This prevents cross-coroutine connection sharing / double-return bugs.
        self._state = _ContextVarConnectionState()

    def _is_closed(self, conn):
        # Enhanced pre-ping: try to detect closed connections more reliably
        try:
            conn.ping(reconnect=False)
            return False
        except Exception:
            return True

    def execute_sql(self, sql, params=None, commit=True):
        for attempt in range(self.max_retries + 1):
            try:
                return super().execute_sql(sql, params, commit)
            except (OperationalError, InterfaceError) as e:
                # https://dev.mysql.com/doc/refman/8.0/en/server-error-reference.html#error_er_lock_deadlock
                # 2013: Lost connection to MySQL server during query
                # 2006: MySQL server has gone away
                # 1213: Deadlock found when trying to get lock; try restarting transaction
                # The issue you are seeing, pymysql.err.InterfaceError: (0, ''), is a known behavior in PyMySQL when the underlying network connection is closed unexpectedly or is in an invalid state, but the error code returned is generic (0).
                error_codes = [2013, 2006, 1213, 0]  # Added 1213 for deadlock, 0 for InterfaceError (0, '')
                error_messages = ['', 'Lost connection', 'Deadlock found']
                should_retry = (
                    (isinstance(e.args, tuple) and len(e.args) > 0 and e.args[0] in error_codes) or
                    (str(e) in error_messages) or
                    (hasattr(e, '__class__') and e.__class__.__name__ == 'InterfaceError')
                )

                if should_retry and attempt < self.max_retries:
                    logging.warning(
                        f"Database connection/deadlock issue (attempt {attempt+1}/{self.max_retries}): {e} {str(e)}"
                    )
                    # Reconnect for connection issues (2013, 2006) and InterfaceError (0)
                    if e.args[0] in error_codes:
                        self._handle_connection_loss()
                    time.sleep(self.retry_delay * (2 ** attempt))
                else:
                    logging.error(f"DB execution failure: {e} {str(e)}")
                    raise
        return None

    def _handle_connection_loss(self):
        # self.close_all()
        # self.connect()
        try:
            self.close()
        except Exception:
            pass
        try:
            self.connect()
        except Exception as e:
            logging.error(f"Failed to reconnect: {e}")
            time.sleep(0.1)
            try:
                self.connect()
            except Exception as e2:
                logging.error(f"Failed to reconnect on second attempt: {e2}")
                raise

    def begin(self):
        for attempt in range(self.max_retries + 1):
            try:
                return super().begin()
            except (OperationalError, InterfaceError) as e:
                error_codes = [2013, 2006, 1213]  # Added 1213 for deadlock
                error_messages = ['', 'Lost connection', 'Deadlock found']

                should_retry = (
                    (hasattr(e, 'args') and e.args and e.args[0] in error_codes) or
                    (str(e) in error_messages) or
                    (hasattr(e, '__class__') and e.__class__.__name__ == 'InterfaceError')
                )

                if should_retry and attempt < self.max_retries:
                    logging.warning(
                        f"Lost connection/deadlock during transaction (attempt {attempt+1}/{self.max_retries}): {e}"
                    )
                    # Reconnect for connection issues (2013, 2006) and InterfaceError (0)
                    if e.args and e.args[0] in [2013, 2006, 0]:
                        self._handle_connection_loss()
                    time.sleep(self.retry_delay * (2 ** attempt))
                else:
                    raise
        return None


class RetryingPooledPostgresqlDatabase(PooledPostgresqlDatabase):
    def __init__(self, *args, **kwargs):
        self.max_retries = kwargs.pop("max_retries", 5)
        self.retry_delay = kwargs.pop("retry_delay", 1)
        super().__init__(*args, **kwargs)
        # Same fix as RetryingPooledMySQLDatabase: isolate connection per coroutine.
        self._state = _ContextVarConnectionState()

    def execute_sql(self, sql, params=None, commit=True):
        for attempt in range(self.max_retries + 1):
            try:
                return super().execute_sql(sql, params, commit)
            except (OperationalError, InterfaceError) as e:
                # PostgreSQL specific error codes
                # 57P01: admin_shutdown
                # 57P02: crash_shutdown
                # 57P03: cannot_connect_now
                # 08006: connection_failure
                # 08003: connection_does_not_exist
                # 08000: connection_exception
                # 40P01: deadlock_detected
                error_messages = ['connection', 'server closed', 'connection refused',
                                'no connection to the server', 'terminating connection', 'deadlock']

                should_retry = any(msg in str(e).lower() for msg in error_messages)

                if should_retry and attempt < self.max_retries:
                    logging.warning(
                        f"PostgreSQL connection/deadlock issue (attempt {attempt+1}/{self.max_retries}): {e}"
                    )
                    # Only reconnect for connection issues, not deadlocks
                    if 'deadlock' not in str(e).lower():
                        self._handle_connection_loss()
                    time.sleep(self.retry_delay * (2 ** attempt))
                else:
                    logging.error(f"PostgreSQL execution failure: {e}")
                    raise
        return None

    def _handle_connection_loss(self):
        try:
            self.close()
        except Exception:
            pass
        try:
            self.connect()
        except Exception as e:
            logging.error(f"Failed to reconnect to PostgreSQL: {e}")
            time.sleep(0.1)
            try:
                self.connect()
            except Exception as e2:
                logging.error(f"Failed to reconnect to PostgreSQL on second attempt: {e2}")
                raise

    def begin(self):
        for attempt in range(self.max_retries + 1):
            try:
                return super().begin()
            except (OperationalError, InterfaceError) as e:
                error_messages = ['connection', 'server closed', 'connection refused',
                                'no connection to the server', 'terminating connection', 'deadlock']

                should_retry = any(msg in str(e).lower() for msg in error_messages)

                if should_retry and attempt < self.max_retries:
                    logging.warning(
                        f"PostgreSQL connection/deadlock lost during transaction (attempt {attempt+1}/{self.max_retries}): {e}"
                    )
                    # Only reconnect for connection issues, not deadlocks
                    if 'deadlock' not in str(e).lower():
                        self._handle_connection_loss()
                    time.sleep(self.retry_delay * (2 ** attempt))
                else:
                    raise
        return None


class RetryingPooledOceanBaseDatabase(PooledMySQLDatabase):
    """Pooled OceanBase database with retry mechanism.

    OceanBase is compatible with MySQL protocol, so we inherit from PooledMySQLDatabase.
    This class provides connection pooling and automatic retry for connection issues.
    """
    def __init__(self, *args, **kwargs):
        self.max_retries = kwargs.pop("max_retries", 5)
        self.retry_delay = kwargs.pop("retry_delay", 1)
        super().__init__(*args, **kwargs)

    def execute_sql(self, sql, params=None, commit=True):
        for attempt in range(self.max_retries + 1):
            try:
                return super().execute_sql(sql, params, commit)
            except (OperationalError, InterfaceError) as e:
                # OceanBase/MySQL specific error codes
                # 2013: Lost connection to MySQL server during query
                # 2006: MySQL server has gone away
                error_codes = [2013, 2006]
                error_messages = ['', 'Lost connection', 'gone away']

                should_retry = (
                    (hasattr(e, 'args') and e.args and e.args[0] in error_codes) or
                    any(msg in str(e).lower() for msg in error_messages) or
                    (hasattr(e, '__class__') and e.__class__.__name__ == 'InterfaceError')
                )

                if should_retry and attempt < self.max_retries:
                    logging.warning(
                        f"OceanBase connection issue (attempt {attempt+1}/{self.max_retries}): {e}"
                    )
                    self._handle_connection_loss()
                    time.sleep(self.retry_delay * (2 ** attempt))
                else:
                    logging.error(f"OceanBase execution failure: {e}")
                    raise
        return None

    def _handle_connection_loss(self):
        try:
            self.close()
        except Exception:
            pass
        try:
            self.connect()
        except Exception as e:
            logging.error(f"Failed to reconnect to OceanBase: {e}")
            time.sleep(0.1)
            try:
                self.connect()
            except Exception as e2:
                logging.error(f"Failed to reconnect to OceanBase on second attempt: {e2}")
                raise

    def begin(self):
        for attempt in range(self.max_retries + 1):
            try:
                return super().begin()
            except (OperationalError, InterfaceError) as e:
                error_codes = [2013, 2006]
                error_messages = ['', 'Lost connection']

                should_retry = (
                    (hasattr(e, 'args') and e.args and e.args[0] in error_codes) or
                    (str(e) in error_messages) or
                    (hasattr(e, '__class__') and e.__class__.__name__ == 'InterfaceError')
                )

                if should_retry and attempt < self.max_retries:
                    logging.warning(
                        f"Lost connection during transaction (attempt {attempt+1}/{self.max_retries})"
                    )
                    self._handle_connection_loss()
                    time.sleep(self.retry_delay * (2 ** attempt))
                else:
                    raise
        return None


class PooledDatabase(Enum):
    MYSQL = RetryingPooledMySQLDatabase
    OCEANBASE = RetryingPooledOceanBaseDatabase
    POSTGRES = RetryingPooledPostgresqlDatabase


class DatabaseMigrator(Enum):
    MYSQL = MySQLMigrator
    OCEANBASE = MySQLMigrator
    POSTGRES = PostgresqlMigrator


@singleton
class BaseDataBase:
    def __init__(self):
        database_config = settings.DATABASE.copy()
        db_name = database_config.pop("name")

        # Extract connection pool parameters from database config (if present)
        # These are defined in service_conf.yaml: max_connections, stale_timeout
        pool_config = {
            'max_retries': 5,
            'retry_delay': 1,
            # Default values: max_connections=100, stale_timeout=30
            'max_connections': 100,
            'stale_timeout': 30,
        }

        # Preserve pool configuration from service_conf.yaml if present
        for key in ['max_connections', 'stale_timeout', 'max_allowed_packet']:
            if key in database_config:
                pool_config[key] = database_config.pop(key)

        # Keep connection parameters (host, port, user, password) for the database connection
        # Add them to pool_config so they are passed to PooledMySQLDatabase
        pool_config.update(database_config)
        self.database_connection = PooledDatabase[settings.DATABASE_TYPE.upper()].value(
            db_name, **pool_config
        )
        # self.database_connection = PooledDatabase[settings.DATABASE_TYPE.upper()].value(db_name, **database_config)
        logging.info("init database on cluster mode successfully")


def with_retry(max_retries=3, retry_delay=1.0):
    """Decorator: Add retry mechanism to database operations

    Args:
        max_retries (int): maximum number of retries
        retry_delay (float): initial retry delay (seconds), will increase exponentially

    Returns:
        decorated function
    """

    def decorator(func):
        @wraps(func)
        def wrapper(*args, **kwargs):
            last_exception = None
            for retry in range(max_retries):
                try:
                    return func(*args, **kwargs)
                except Exception as e:
                    last_exception = e
                    # get self and method name for logging
                    self_obj = args[0] if args else None
                    func_name = func.__name__
                    lock_name = getattr(self_obj, "lock_name", "unknown") if self_obj else "unknown"

                    if retry < max_retries - 1:
                        current_delay = retry_delay * (2**retry)
                        logging.warning(f"{func_name} {lock_name} failed: {str(e)}, retrying ({retry + 1}/{max_retries})")
                        time.sleep(current_delay)
                    else:
                        logging.error(f"{func_name} {lock_name} failed after all attempts: {str(e)}")

            if last_exception:
                raise last_exception
            return False

        return wrapper

    return decorator


class PostgresDatabaseLock:
    def __init__(self, lock_name, timeout=10, db=None):
        self.lock_name = lock_name
        self.lock_id = int(hashlib.md5(lock_name.encode()).hexdigest(), 16) % (2**31 - 1)
        self.timeout = int(timeout)
        self.db = db if db else DB

    @with_retry(max_retries=3, retry_delay=1.0)
    def lock(self):
        cursor = self.db.execute_sql("SELECT pg_try_advisory_lock(%s)", (self.lock_id,))
        ret = cursor.fetchone()
        if ret[0] == 0:
            raise Exception(f"acquire postgres lock {self.lock_name} timeout")
        elif ret[0] == 1:
            return True
        else:
            raise Exception(f"failed to acquire lock {self.lock_name}")

    @with_retry(max_retries=3, retry_delay=1.0)
    def unlock(self):
        cursor = self.db.execute_sql("SELECT pg_advisory_unlock(%s)", (self.lock_id,))
        ret = cursor.fetchone()
        if ret[0] == 0:
            raise Exception(f"postgres lock {self.lock_name} was not established by this thread")
        elif ret[0] == 1:
            return True
        else:
            raise Exception(f"postgres lock {self.lock_name} does not exist")

    def __enter__(self):
        if isinstance(self.db, PooledPostgresqlDatabase):
            self.lock()
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        if isinstance(self.db, PooledPostgresqlDatabase):
            self.unlock()

    def __call__(self, func):
        @wraps(func)
        def magic(*args, **kwargs):
            with self:
                return func(*args, **kwargs)

        return magic


class MysqlDatabaseLock:
    def __init__(self, lock_name, timeout=10, db=None):
        self.lock_name = lock_name
        self.timeout = int(timeout)
        self.db = db if db else DB

    @with_retry(max_retries=3, retry_delay=1.0)
    def lock(self):
        # SQL parameters only support %s format placeholders
        cursor = self.db.execute_sql("SELECT GET_LOCK(%s, %s)", (self.lock_name, self.timeout))
        ret = cursor.fetchone()
        if ret[0] == 0:
            raise Exception(f"acquire mysql lock {self.lock_name} timeout")
        elif ret[0] == 1:
            return True
        else:
            raise Exception(f"failed to acquire lock {self.lock_name}")

    @with_retry(max_retries=3, retry_delay=1.0)
    def unlock(self):
        cursor = self.db.execute_sql("SELECT RELEASE_LOCK(%s)", (self.lock_name,))
        ret = cursor.fetchone()
        if ret[0] == 0:
            raise Exception(f"mysql lock {self.lock_name} was not established by this thread")
        elif ret[0] == 1:
            return True
        else:
            raise Exception(f"mysql lock {self.lock_name} does not exist")

    def __enter__(self):
        if isinstance(self.db, PooledMySQLDatabase):
            self.lock()
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        if isinstance(self.db, PooledMySQLDatabase):
            self.unlock()

    def __call__(self, func):
        @wraps(func)
        def magic(*args, **kwargs):
            with self:
                return func(*args, **kwargs)

        return magic


class DatabaseLock(Enum):
    MYSQL = MysqlDatabaseLock
    OCEANBASE = MysqlDatabaseLock
    POSTGRES = PostgresDatabaseLock


DB = BaseDataBase().database_connection
DB.lock = DatabaseLock[settings.DATABASE_TYPE.upper()].value


def close_connection():
    """Return the current coroutine's connection to the pool, then clean up stale ones.

    With _ContextVarConnectionState, DB.is_closed() / DB.close() operate on the
    calling coroutine's own connection (not a shared threading.local).  We call
    DB.close() to return the connection to the pool immediately after each request
    rather than waiting for the stale_timeout GC cycle.  DB.close_stale(age=30)
    is kept as a safety net for any orphaned connections (e.g. background tasks
    that do not go through teardown_request).
    """
    try:
        if DB:
            if not DB.is_closed():
                DB.close()          # return this coroutine's connection to pool
            DB.close_stale(age=30)  # GC any orphaned connections older than 30s
    except Exception as e:
        logging.exception(e)


class DataBaseModel(BaseModel):
    class Meta:
        database = DB


@DB.connection_context()
@DB.lock("init_database_tables", 60)
def init_database_tables(alter_fields=[]):
    members = inspect.getmembers(sys.modules[__name__], inspect.isclass)
    table_objs = []
    create_failed_list = []
    for name, obj in members:
        if obj != DataBaseModel and issubclass(obj, DataBaseModel):
            table_objs.append(obj)

            if not obj.table_exists():
                logging.debug(f"start create table {obj.__name__}")
                try:
                    obj.create_table(safe=True)
                    logging.debug(f"create table success: {obj.__name__}")
                except Exception as e:
                    logging.exception(e)
                    create_failed_list.append(obj.__name__)
            else:
                logging.debug(f"table {obj.__name__} already exists, skip creation.")

    if create_failed_list:
        logging.error(f"create tables failed: {create_failed_list}")
        raise Exception(f"create tables failed: {create_failed_list}")
    migrate_db()


def fill_db_model_object(model_object, human_model_dict):
    for k, v in human_model_dict.items():
        attr_name = "%s" % k
        if hasattr(model_object.__class__, attr_name):
            setattr(model_object, attr_name, v)
    return model_object


class User(DataBaseModel, AuthUser):
    id = CharField(max_length=32, primary_key=True)
    access_token = CharField(max_length=255, null=True, index=True)
    nickname = CharField(max_length=100, null=False, help_text="nicky name", index=True)
    password = CharField(max_length=255, null=True, help_text="password", index=True)
    email = CharField(max_length=255, null=False, help_text="email", unique=True)
    avatar = TextField(null=True, help_text="avatar base64 string")
    language = CharField(max_length=32, null=True, help_text="English|Chinese", default="Chinese" if "zh_CN" in os.getenv("LANG", "") else "English", index=True)
    color_schema = CharField(max_length=32, null=True, help_text="Bright|Dark", default="Bright", index=True)
    timezone = CharField(max_length=64, null=True, help_text="Timezone", default="UTC+8\tAsia/Shanghai", index=True)
    last_login_time = DateTimeField(null=True, index=True)
    is_authenticated = CharField(max_length=1, null=False, default="1", index=True)
    is_active = CharField(max_length=1, null=False, default="1", index=True)
    is_anonymous = CharField(max_length=1, null=False, default="0", index=True)
    login_channel = CharField(null=True, help_text="from which user login", index=True)
    status = CharField(max_length=1, null=True, help_text="is it validate(0: wasted, 1: validate)", default="1", index=True)
    is_superuser = BooleanField(null=True, help_text="is root", default=False, index=True)
    role_id = IntegerField(null=False, help_text="id in rag_flow.role", index=True, default=1)

    def __str__(self):
        return self.email

    def get_id(self):
        jwt = Serializer(secret_key=settings.SECRET_KEY)
        return jwt.dumps(str(self.access_token))

    class Meta:
        db_table = "user"


class Role(DataBaseModel):
    id = PrimaryKeyField()
    role_name = CharField(max_length=64, null=False, help_text="owner|public", index=True)
    description = TextField(null=True, help_text="role description", index=False)

    class Meta:
        db_table = "role"


class RoleResource(DataBaseModel):
    role_id = IntegerField(null=False, index=True)
    resource_type = BigIntegerField(null=False, help_text="resource type", index=False)
    action = IntegerField(null=False, help_text="action", index=False)

    class Meta:
        db_table = "role_resource"


class Tenant(DataBaseModel):
    id = CharField(max_length=32, primary_key=True)
    name = CharField(max_length=100, null=True, help_text="Tenant name", index=True)
    public_key = CharField(max_length=255, null=True, index=True)
    llm_id = CharField(max_length=128, null=False, help_text="default llm ID", index=True)
    tenant_llm_id = IntegerField(null=True, help_text="id in tenant_llm", index=True)
    embd_id = CharField(max_length=128, null=False, help_text="default embedding model ID", index=True)
    tenant_embd_id = IntegerField(null=True, help_text="id in tenant_llm", index=True)
    asr_id = CharField(max_length=128, null=False, help_text="default ASR model ID", index=True)
    tenant_asr_id = IntegerField(null=True, help_text="id in tenant_llm", index=True)
    img2txt_id = CharField(max_length=128, null=False, help_text="default image to text model ID", index=True)
    tenant_img2txt_id = IntegerField(null=True, help_text="id in tenant_llm", index=True)
    rerank_id = CharField(max_length=128, null=False, help_text="default rerank model ID", index=True)
    tenant_rerank_id = IntegerField(null=True, help_text="id in tenant_llm", index=True)
    tts_id = CharField(max_length=256, null=True, help_text="default tts model ID", index=True)
    tenant_tts_id = IntegerField(null=True, help_text="id in tenant_llm", index=True)
    parser_ids = CharField(max_length=256, null=False, help_text="document processors", index=True)
    credit = IntegerField(default=512, index=True)
    status = CharField(max_length=1, null=True, help_text="is it validate(0: wasted, 1: validate)", default="1", index=True)

    class Meta:
        db_table = "tenant"


class UserTenant(DataBaseModel):
    id = CharField(max_length=32, primary_key=True)
    user_id = CharField(max_length=32, null=False, index=True)
    tenant_id = CharField(max_length=32, null=False, index=True)
    role = CharField(max_length=32, null=False, help_text="UserTenantRole", index=True)
    invited_by = CharField(max_length=32, null=False, index=True)
    status = CharField(max_length=1, null=True, help_text="is it validate(0: wasted, 1: validate)", default="1", index=True)

    class Meta:
        db_table = "user_tenant"


class Group(DataBaseModel):
    id = CharField(max_length=32, primary_key=True)
    name = CharField(max_length=100, null=True, help_text="Group name", index=True)
    avatar = TextField(null=True, default="default.avatar", help_text="avatar base64 string")

    owner_id = CharField(max_length=32, null=False, index=True)  # UserTenant ID
    tenant_id = CharField(max_length=32, null=False, index=True)  # Tenant ID
    status = CharField(max_length=1, null=True, help_text="is it validate(0: wasted, 1: validate)", default="1", index=True)

    class Meta:
        db_table = "group_info"


class GroupMember(DataBaseModel):
    id = CharField(max_length=32, primary_key=True)

    member_id = CharField(max_length=32, null=False, index=True)  # UserTenant ID
    group_id = CharField(max_length=32, null=False, index=True)  # Group ID
    role = CharField(max_length=32, choices=[(r.value, r.name) for r in TeamRole.__members__.values()], null=False, help_text="GroupRole", index=True)
    status = CharField(max_length=1, null=True, help_text="is it validate(0: wasted, 1: validate)", default="1", index=True)

    class Meta:
        db_table = "group_member"


class Department(DataBaseModel):
    id = CharField(max_length=32, primary_key=True)
    name = CharField(max_length=100, null=False, help_text="Department name", index=True)
    description = TextField(null=True, help_text="Department description")
    avatar = TextField(null=True, help_text="avatar base64 string")
    path = CharField(max_length=255, unique=True, help_text="Department hierarchy path")
    formatted_path = CharField(max_length=255, help_text="Department hierarchy string path")
    parent_id = CharField(max_length=32, null=False, index=True)  # Department ID
    owner_id = CharField(max_length=32, null=False, index=True)  # UserTenant ID
    tenant_id = CharField(max_length=32, null=False, index=True)  # Tenant ID
    status = CharField(max_length=1, null=True, help_text="is it validate(0: wasted, 1: validate)", default="1", index=True)

    class Meta:
        db_table = "department"

    def update_department_path(self):
        if self.parent:
            parent = self.parent
            self.path = f"{parent.path}/{self.id}"
        else:
            self.path = self.id  # Root department path = its own ID
        super().save()


class DepartmentMember(DataBaseModel):
    id = CharField(max_length=32, primary_key=True)
    role = CharField(max_length=32, choices=[(r.value, r.name) for r in TeamRole.__members__.values()], null=False, help_text="DepartmentRole", index=True)
    status = CharField(max_length=1, null=True, help_text="is it validate(0: wasted, 1: validate)", default="1", index=True)
    member_id = CharField(max_length=32, null=False, index=True)  # UserTenant ID
    department_id = CharField(max_length=32, null=False, index=True)  # Department ID

    class Meta:
        db_table = "department_member"


class Permission(DataBaseModel):
    id = CharField(max_length=32, primary_key=True)
    member_id = CharField(max_length=32, null=True, index=True)  # UserTenant ID
    group_id = CharField(max_length=32, null=True, index=True)  # Group ID
    department_id = CharField(max_length=32, null=True, index=True)  # Department ID
    tenant_id = CharField(max_length=32, null=False, index=True)  # Tenant ID
    resource_type = CharField(max_length=32, choices=[(t.value, t.name) for t in ResourceType.__members__.values()], null=False, index=True)
    resource_id = CharField(max_length=32, null=True, index=True)
    permission = IntegerField(null=False, default=PermissionValue.PERMISSION_NULL.value, help_text="Permission", index=True)
    status = CharField(
        max_length=1,
        null=True,
        help_text="is it validate(0: wasted, 1: validate)",
        default="1",
        index=True)

    class Meta:
        db_table = "permission"


class PermissionChangeLog(DataBaseModel):
    id = CharField(max_length=32, primary_key=True)
    tenant_id = CharField(max_length=32, null=False, index=True, help_text="Tenant ID")
    operator_id = CharField(max_length=32, null=False, index=True, help_text="UserTenant ID")
    target_type = CharField(max_length=20, null=False, choices=VALID_PERMISSION_TARGET_TYPES)
    target_id = CharField(max_length=32, null=False)
    resource_type = CharField(max_length=32, null=False, choices=VALID_RESOURCE_TYPES)
    resource_id = CharField(max_length=32, null=False)
    old_permission = IntegerField(null=False)
    new_permission = IntegerField(null=False)
    action_type = CharField(max_length=20, null=False, choices=VALID_PERMISSION_ACTION_TYPES)
    reason = TextField(null=True)

    class Meta:
        table_name = "permission_change_log"


class InvitationCode(DataBaseModel):
    id = CharField(max_length=32, primary_key=True)
    code = CharField(max_length=32, null=False, index=True)
    visit_time = DateTimeField(null=True, index=True)
    user_id = CharField(max_length=32, null=True, index=True)
    tenant_id = CharField(max_length=32, null=True, index=True)
    status = CharField(max_length=1, null=True, help_text="is it validate(0: wasted, 1: validate)", default="1", index=True)

    class Meta:
        db_table = "invitation_code"


class LLMFactories(DataBaseModel):
    name = CharField(max_length=128, null=False, help_text="LLM factory name", primary_key=True)
    logo = TextField(null=True, help_text="llm logo base64")
    tags = CharField(max_length=255, null=False, help_text="LLM, Text Embedding, Image2Text, ASR", index=True)
    rank = IntegerField(default=0, index=False)
    status = CharField(max_length=1, null=True, help_text="is it validate(0: wasted, 1: validate)", default="1", index=True)

    def __str__(self):
        return self.name

    class Meta:
        db_table = "llm_factories"


class LLM(DataBaseModel):
    # LLMs dictionary
    llm_name = CharField(max_length=128, null=False, help_text="LLM name", index=True)
    model_type = CharField(max_length=128, null=False, help_text="LLM, Text Embedding, Image2Text, ASR", index=True)
    fid = CharField(max_length=128, null=False, help_text="LLM factory id", index=True)
    max_tokens = IntegerField(default=0)

    tags = CharField(max_length=255, null=False, help_text="LLM, Text Embedding, Image2Text, Chat, 32k...", index=True)
    is_tools = BooleanField(null=False, help_text="support tools", default=False)
    status = CharField(max_length=1, null=True, help_text="is it validate(0: wasted, 1: validate)", default="1", index=True)

    def __str__(self):
        return self.llm_name

    class Meta:
        primary_key = CompositeKey("fid", "llm_name")
        db_table = "llm"


class TenantLLM(DataBaseModel):
    id = PrimaryKeyField()
    tenant_id = CharField(max_length=32, null=False, index=True)
    llm_factory = CharField(max_length=128, null=False, help_text="LLM factory name", index=True)
    model_type = CharField(max_length=128, null=True, help_text="LLM, Text Embedding, Image2Text, ASR", index=True)
    llm_name = CharField(max_length=128, null=True, help_text="LLM name", default="", index=True)
    api_key = CharField(max_length=8192, null=True, help_text="API KEY")
    api_base = CharField(max_length=255, null=True, help_text="API Base")
    max_tokens = IntegerField(default=8192, help_text="Max context token num", index=True)
    used_tokens = IntegerField(default=0, help_text="Used token num", index=True)
    status = CharField(max_length=1, null=False, help_text="is it validate(0: wasted, 1: validate)", default="1", index=True)

    def __str__(self):
        return self.llm_name

    class Meta:
        db_table = "tenant_llm"
        indexes = (
            (("tenant_id", "llm_factory", "llm_name"), True),
        )


class RoleDefaultModel(DataBaseModel):
    role_id = IntegerField(null=False, default=0, help_text="id in rag_flow.role", index=True)
    model_type = CharField(max_length=128, null=False, default="", help_text="LLM, Embedding, Image2Text, ASR, RERANK, TTS", index=True)
    model_id = CharField(max_length=128, null=False, default="", help_text="in format 'model_name@factory'", index=True)
    tenant_id = CharField(max_length=32, null=False, help_text="tenant_id of the model's tenant", index=True)

    class Meta:
        db_table = "role_default_model"


class TenantLangfuse(DataBaseModel):
    tenant_id = CharField(max_length=32, null=False, primary_key=True)
    secret_key = CharField(max_length=2048, null=False, help_text="SECRET KEY", index=True)
    public_key = CharField(max_length=2048, null=False, help_text="PUBLIC KEY", index=True)
    host = CharField(max_length=128, null=False, help_text="HOST", index=True)

    def __str__(self):
        return "Langfuse host" + self.host

    class Meta:
        db_table = "tenant_langfuse"


class Knowledgebase(DataBaseModel):
    id = CharField(max_length=32, primary_key=True)
    avatar = TextField(null=True, help_text="avatar base64 string")
    tenant_id = CharField(max_length=32, null=False, index=True)
    name = CharField(max_length=128, null=False, help_text="KB name", index=True)
    language = CharField(max_length=32, null=True, default="Chinese" if "zh_CN" in os.getenv("LANG", "") else "English", help_text="English|Chinese", index=True)
    description = TextField(null=True, help_text="KB description")
    embd_id = CharField(max_length=128, null=False, help_text="default embedding model ID", index=True)
    tenant_embd_id = IntegerField(null=True, help_text="id in tenant_llm", index=True)
    permission = CharField(max_length=16, null=False, help_text="me|team", default="me", index=True)
    created_by = CharField(max_length=32, null=False, index=True)
    doc_num = IntegerField(default=0, index=True)
    token_num = IntegerField(default=0, index=True)
    chunk_num = IntegerField(default=0, index=True)
    similarity_threshold = FloatField(default=0.2, index=True)
    vector_similarity_weight = FloatField(default=0.3, index=True)

    parser_id = CharField(max_length=32, null=False, help_text="default parser ID", default=ParserType.NAIVE.value, index=True)
    pipeline_id = CharField(max_length=32, null=True, help_text="Pipeline ID", index=True)
    parser_config = JSONField(null=False, default={"pages": [[1, 1000000]], "table_context_size": 0, "image_context_size": 0})
    pagerank = IntegerField(default=0, index=False)

    graphrag_task_id = CharField(max_length=32, null=True, help_text="Graph RAG task ID", index=True)
    graphrag_task_finish_at = DateTimeField(null=True)
    raptor_task_id = CharField(max_length=32, null=True, help_text="RAPTOR task ID", index=True)
    raptor_task_finish_at = DateTimeField(null=True)
    mindmap_task_id = CharField(max_length=32, null=True, help_text="Mindmap task ID", index=True)
    mindmap_task_finish_at = DateTimeField(null=True)
    embed_task_id = CharField(max_length=32, null=True, help_text="Switch embedding task ID", index=True)
    embed_task_finish_at = DateTimeField(null=True)
    clone_task_id = CharField(max_length=32, null=True, help_text="Duplicate dataset task ID", index=True)
    clone_task_finish_at = DateTimeField(null=True)

    status = CharField(max_length=1, null=True, help_text="is it validate(0: wasted, 1: validate)", default="1", index=True)

    def __str__(self):
        return self.name

    class Meta:
        db_table = "knowledgebase"


class Document(DataBaseModel):
    id = CharField(max_length=32, primary_key=True)
    thumbnail = TextField(null=True, help_text="thumbnail base64 string")
    kb_id = CharField(max_length=256, null=False, index=True)
    parser_id = CharField(max_length=32, null=False, help_text="default parser ID", index=True)
    pipeline_id = CharField(max_length=32, null=True, help_text="pipeline ID", index=True)
    parser_config = JSONField(null=False, default={"pages": [[1, 1000000]], "table_context_size": 0, "image_context_size": 0})
    source_type = CharField(max_length=128, null=False, default="local", help_text="where dose this document come from", index=True)
    type = CharField(max_length=32, null=False, help_text="file extension", index=True)
    created_by = CharField(max_length=32, null=False, help_text="who created it", index=True)
    name = CharField(max_length=255, null=True, help_text="file name", index=True)
    location = CharField(max_length=255, null=True, help_text="where dose it store", index=True)
    size = IntegerField(default=0, index=True)
    token_num = IntegerField(default=0, index=True)
    chunk_num = IntegerField(default=0, index=True)
    progress = FloatField(default=0, index=True)
    progress_msg = TextField(null=True, help_text="process message", default="")
    process_begin_at = DateTimeField(null=True, index=True)
    process_duration = FloatField(default=0)
    suffix = CharField(max_length=32, null=False, help_text="The real file extension suffix", index=True)
    from_kb_id = CharField(max_length=256, null=True, index=True)

    content_hash = CharField(max_length=32, null=True, help_text="xxhash128 of document content for change detection", default="", index=True)

    run = CharField(max_length=1, null=True, help_text="start to run processing or cancel.(1: run it; 2: cancel)", default="0", index=True)
    status = CharField(max_length=1, null=True, help_text="is it validate(0: wasted, 1: validate)", default="1", index=True)

    class Meta:
        db_table = "document"


class File(DataBaseModel):
    id = CharField(max_length=32, primary_key=True)
    parent_id = CharField(max_length=32, null=False, help_text="parent folder id", index=True)
    tenant_id = CharField(max_length=32, null=False, help_text="tenant id", index=True)
    created_by = CharField(max_length=32, null=False, help_text="who created it", index=True)
    name = CharField(max_length=255, null=False, help_text="file name or folder name", index=True)
    location = CharField(max_length=255, null=True, help_text="where dose it store", index=True)
    size = IntegerField(default=0, index=True)
    type = CharField(max_length=32, null=False, help_text="file extension", index=True)
    source_type = CharField(max_length=128, null=False, default="", help_text="where dose this document come from", index=True)

    class Meta:
        db_table = "file"


class File2Document(DataBaseModel):
    id = CharField(max_length=32, primary_key=True)
    file_id = CharField(max_length=32, null=True, help_text="file id", index=True)
    document_id = CharField(max_length=32, null=True, help_text="document id", index=True)

    class Meta:
        db_table = "file2document"


class Task(DataBaseModel):
    id = CharField(max_length=32, primary_key=True)
    doc_id = CharField(max_length=32, null=False, index=True)
    from_page = IntegerField(default=0)
    to_page = IntegerField(default=100000000)
    task_type = CharField(max_length=32, null=False, default="")
    priority = IntegerField(default=0)

    begin_at = DateTimeField(null=True, index=True)
    process_duration = FloatField(default=0)

    progress = FloatField(default=0, index=True)
    progress_msg = TextField(null=True, help_text="process message", default="")
    retry_count = IntegerField(default=0)
    digest = TextField(null=True, help_text="task digest", default="")
    chunk_ids = LongTextField(null=True, help_text="chunk ids", default="")


class Dialog(DataBaseModel):
    id = CharField(max_length=32, primary_key=True)
    tenant_id = CharField(max_length=32, null=False, index=True)
    name = CharField(max_length=255, null=True, help_text="dialog application name", index=True)
    description = TextField(null=True, help_text="Dialog description")
    icon = TextField(null=True, help_text="icon base64 string")
    language = CharField(max_length=32, null=True, default="Chinese" if "zh_CN" in os.getenv("LANG", "") else "English", help_text="English|Chinese", index=True)
    llm_id = CharField(max_length=128, null=False, help_text="default llm ID")
    tenant_llm_id = IntegerField(null=True, help_text="id in tenant_llm", index=True)

    llm_setting = JSONField(null=False, default={"temperature": 0.1, "top_p": 0.3, "frequency_penalty": 0.7, "presence_penalty": 0.4, "max_tokens": 512})
    prompt_type = CharField(max_length=16, null=False, default="simple", help_text="simple|advanced", index=True)
    prompt_config = JSONField(
        null=False,
        default={"system": "", "prologue": "Hi! I'm your assistant. What can I do for you?", "parameters": [], "empty_response": "Sorry! No relevant content was found in the knowledge base!"},
    )
    meta_data_filter = JSONField(null=True, default={})

    similarity_threshold = FloatField(default=0.2)
    vector_similarity_weight = FloatField(default=0.3)

    top_n = IntegerField(default=6)

    top_k = IntegerField(default=1024)

    do_refer = CharField(max_length=1, null=False, default="1", help_text="it needs to insert reference index into answer or not")

    rerank_id = CharField(max_length=128, null=False, help_text="default rerank model ID")
    tenant_rerank_id = IntegerField(null=True, help_text="id in tenant_llm", index=True)
    kb_ids = JSONField(null=False, default=[])
    status = CharField(max_length=1, null=True, help_text="is it validate(0: wasted, 1: validate)", default="1", index=True)

    class Meta:
        db_table = "dialog"


class Conversation(DataBaseModel):
    id = CharField(max_length=32, primary_key=True)
    dialog_id = CharField(max_length=32, null=False, index=True)
    name = CharField(max_length=255, null=True, help_text="conversation name", index=True)
    message = JSONField(null=True)
    reference = JSONField(null=True, default=[])
    user_id = CharField(max_length=255, null=True, help_text="user_id", index=True)

    class Meta:
        db_table = "conversation"


class APIToken(DataBaseModel):
    tenant_id = CharField(max_length=32, null=False, index=True)
    token = CharField(max_length=255, null=False, index=True)
    dialog_id = CharField(max_length=32, null=True, index=True)
    source = CharField(max_length=16, null=True, help_text="none|agent|dialog", index=True)
    beta = CharField(max_length=255, null=True, index=True)

    class Meta:
        db_table = "api_token"
        primary_key = CompositeKey("tenant_id", "token")


class API4Conversation(DataBaseModel):
    id = CharField(max_length=32, primary_key=True)
    name = CharField(max_length=255, null=True, help_text="conversation name", index=False)
    dialog_id = CharField(max_length=32, null=False, index=True)
    user_id = CharField(max_length=255, null=False, help_text="user_id", index=True)
    exp_user_id = CharField(max_length=255, null=True, help_text="exp_user_id", index=True)
    message = JSONField(null=True)
    reference = JSONField(null=True, default=[])
    tokens = IntegerField(default=0)
    source = CharField(max_length=16, null=True, help_text="none|agent|dialog", index=True)
    dsl = JSONField(null=True, default={})
    duration = FloatField(default=0, index=True)
    round = IntegerField(default=0, index=True)
    thumb_up = IntegerField(default=0, index=True)
    errors = TextField(null=True, help_text="errors")
    version_title = CharField(max_length=255, null=True, help_text="canvas version title when session created", index=False)

    class Meta:
        db_table = "api_4_conversation"


class UserCanvas(DataBaseModel):
    id = CharField(max_length=32, primary_key=True)
    avatar = TextField(null=True, help_text="avatar base64 string")
    user_id = CharField(max_length=255, null=False, help_text="user_id", index=True)
    title = CharField(max_length=255, null=True, help_text="Canvas title")

    permission = CharField(max_length=16, null=False, help_text="me|team", default="me", index=True)
    release = BooleanField(null=False, help_text="is released", default=False, index=True)
    description = TextField(null=True, help_text="Canvas description")
    canvas_type = CharField(max_length=32, null=True, help_text="Canvas type", index=True)
    canvas_category = CharField(max_length=32, null=False, default="agent_canvas", help_text="Canvas category: agent_canvas|dataflow_canvas", index=True)
    dsl = JSONField(null=True, default={})

    class Meta:
        db_table = "user_canvas"


class CanvasTemplate(DataBaseModel):
    id = CharField(max_length=32, primary_key=True)
    avatar = TextField(null=True, help_text="avatar base64 string")
    title = JSONField(null=True, default=dict, help_text="Canvas title")
    description = JSONField(null=True, default=dict, help_text="Canvas description")
    canvas_type = CharField(max_length=32, null=True, help_text="Canvas type", index=True)
    canvas_category = CharField(max_length=32, null=False, default="agent_canvas", help_text="Canvas category: agent_canvas|dataflow_canvas", index=True)
    dsl = JSONField(null=True, default={})

    class Meta:
        db_table = "canvas_template"


class Product(DataBaseModel):
    id = CharField(max_length=32, primary_key=True)
    name = CharField(null=False, max_length=255, index=True)
    quota_apps = IntegerField(null=False, help_text="Limit number of APP of the tenant, Chat, Search, Agent")
    quota_members = IntegerField(null=False, help_text="Limit number of members of the tenant")
    quota_kb_storage = BigIntegerField(null=False, help_text="Limit dataset storage bytes of the tenant")
    task_priority = CharField(null=False, help_text="Task priority of the tenant")
    price_ids = TextField(null=False, default="", help_text="price ids on stripe.com")
    description = TextField(null=True)
    product_type = CharField(null=False, choices=["subscription", "usage_based"])
    usage_stat_type = CharField(null=True, choices=["before", "after"])  # only for usage_based
    version = IntegerField(null=False, help_text="Product version")
    quota_points = BigIntegerField(null=True, help_text="Monthly point quota for subscription plans")

    class Meta:
        db_table = "billing_product"


class QuotaItem(DataBaseModel):
    """Product associated many QuotaItem"""

    id = CharField(max_length=32, primary_key=True)
    product_id = CharField(max_length=32, index=True)
    quota_type = CharField(null=False, choices=["app_total", "team_seat", "api_qps", "kb_storage"])
    quantity = BigIntegerField(null=False)
    unit = CharField(null=False, choices=["apps", "seats", "calls", "bytes"])
    description = TextField(null=True)

    class Meta:
        db_table = "billing_quota_item"


class PricePoint(DataBaseModel):
    """product -> price point"""

    id = CharField(max_length=32, primary_key=True)
    product_id = CharField(max_length=32, index=True)
    product_name = CharField(null=False, max_length=255)
    price_type = CharField(null=False, choices=["subscription", "usage_based"], index=True)
    billing_frequency = CharField(null=False, choices=["monthly", "yearly", "one_time"])
    included_free_amount = IntegerField(null=True)  # ???
    unit = CharField(choices=["token", "page"], null=True)
    unit_quantity = IntegerField(null=True)
    price_amount = IntegerField(null=True)  # price in cents (e.g., 100 = $1.00)
    price_currency = CharField(max_length=3, null=True)  # usd, cny
    consuming_point_amount = IntegerField(null=False, default=0)
    effective_time = DateTimeField(null=False)
    expiry_time = DateTimeField(null=True)

    class Meta:
        db_table = "billing_pricepoint"


class ProductUsageTracing(DataBaseModel):
    id = CharField(max_length=32, primary_key=True)
    tenant_id = CharField(max_length=32, null=False, index=True)
    product_id = CharField(max_length=32, null=False, index=True)
    price_point_id = CharField(max_length=32, null=False, index=True)  # price point table
    task_quantity = IntegerField()
    total_cost_cents = BigIntegerField(null=False, default=0)
    currency = CharField(max_length=3)  # usd, cny
    status = CharField(null=False, choices=[item.value for item in UsageTraceStatus])  # Our usage trace status
    description = TextField(null=True)

    class Meta:
        db_table = "billing_product_usage_tracing"


class PurchasedProductOverview(DataBaseModel):
    "User can purchase many products"

    id = CharField(max_length=32, primary_key=True)
    tenant_id = CharField(max_length=32, null=False, index=True)
    product_id = CharField(max_length=32, null=False, index=True)  # QUESTION: should be delete?
    product_name = CharField(null=False, max_length=255)
    quantity = IntegerField(null=False, constraints=[Check("quantity >= 0")])
    effective_time = DateTimeField(null=False)
    expiry_time = DateTimeField(null=True)

    # QUESTION: union primary key? (tenant_id, product_name)

    class Meta:
        db_table = "billing_purchased_product_overview"


class PaymentOrder(DataBaseModel):
    "Stripe checkout recording"

    id = CharField(max_length=32, primary_key=True)
    tenant_id = CharField(max_length=32, null=False, index=True)
    customer_id = CharField(max_length=255, null=False, default="", help_text="customer id on stripe.com", index=True)
    payment_type = CharField(choices=["subscription", "usage_based"])
    product_id = CharField(max_length=32, null=True, index=True)  # Optional for recharge
    product_name = CharField(null=False, max_length=255)
    is_prorated = BooleanField(default=False)

    # stripe
    amount_cents = BigIntegerField(null=False, default=0)
    currency = CharField(max_length=3, null=False, choices=["usd", "cny"])
    payment_method = CharField(null=False, choices=["card"])

    order_id = CharField(max_length=128, null=False, help_text="Stripe checkout order id")
    price_id = CharField(max_length=128, null=False, help_text="stripe subscription price_id")
    payment_intent_id = CharField(max_length=128, null=True, help_text="Stripe payment intent id, for one-off")
    payment_subscription_id = CharField(max_length=128, null=True, help_text="Stripe payment subscription id, for subscription")
    receipt_url = CharField(max_length=512, null=True, help_text="invoice")
    receipt_pdf_url = CharField(max_length=512, null=True)
    payment_channel = CharField(null=False, choices=["stripe"])
    payment_status = CharField(null=False, choices=[item.value for item in PaymentStatus], help_text="Our payment status")
    stripe_status = CharField(max_length=255, null=False, default="", help_text="raw status from stripe")
    paid = BooleanField()
    captured = BooleanField()

    description = TextField(null=True)

    order_created_at = DateTimeField(help_text="stripe checkout create at")
    payment_detail = JSONField(null=True, default={})

    class Meta:
        db_table = "billing_payment_order"


class BillingWebhookEvent(DataBaseModel):
    "Stripe webhook audit log"

    id = CharField(max_length=32, primary_key=True)
    event_id = CharField(max_length=255, null=False, unique=True, index=True)
    event_type = CharField(max_length=255, null=False, index=True)
    object_id = CharField(max_length=255, null=True, index=True)
    payload = JSONField(null=False)
    created_at = DateTimeField(null=True)
    received_at = DateTimeField(null=False)

    class Meta:
        db_table = "billing_webhook_event"


class Subscription(DataBaseModel):
    """
    A tenant can have only one subscription
    """

    id = CharField(max_length=32, primary_key=True)
    tenant_id = CharField(max_length=32, null=False, index=True)
    product_id = CharField(max_length=32, null=False)
    plan_name = CharField(max_length=255, null=False, default="trial", help_text="billing plan", index=True)
    order_id = CharField(max_length=128, null=False, help_text="from RAGFlow Order")
    status = CharField(choices=["active", "canceled", "expired", "pending"])

    customer_id = CharField(max_length=255, null=False, default="", help_text="customer id on stripe.com", index=True)
    price_id = CharField(max_length=128, null=False, help_text="stripe subscription price_id")
    subscription_id = CharField(max_length=255, null=False, default="", help_text="subscription id on stripe.com", index=True)
    subscription_status = CharField(max_length=255, null=False, default="", help_text="subscription status on stripe.com", index=True)
    invoice_id = CharField(max_length=255, null=True, default="", help_text="invoice id on stripe.com", index=True)
    invoice_url = CharField(max_length=512, null=True, help_text="invoice")
    invoice_pdf_url = CharField(max_length=512, null=True)

    start_time = DateTimeField(null=False)
    end_time = DateTimeField(null=True)
    renew_time = DateTimeField(null=True)
    original_subscription_id = CharField(max_length=32)

    class Meta:
        db_table = "billing_subscription"


class StorageSubscription(DataBaseModel):
    """
    One storage add-on subscription per tenant.
    `addon_storage_bytes` is the currently usable quota.
    `target_quantity_bytes` is the desired quota after pending changes settle.
    """

    id = CharField(max_length=32, primary_key=True)
    tenant_id = CharField(max_length=32, null=False, index=True, unique=True)
    customer_id = CharField(max_length=255, null=False, default="", index=True)

    subscription_id = CharField(max_length=255, null=False, default="", index=True)
    subscription_item_id = CharField(max_length=255, null=False, default="")
    price_id = CharField(max_length=128, null=False, default="")
    schedule_id = CharField(max_length=255, null=False, default="")

    addon_storage_bytes = BigIntegerField(null=False, default=0, constraints=[Check("addon_storage_bytes >= 0")])
    target_quantity_bytes = BigIntegerField(null=False, default=0, constraints=[Check("target_quantity_bytes >= 0")])
    pending_quantity_bytes = BigIntegerField(null=True, constraints=[Check("pending_quantity_bytes >= 0")])
    pending_effective_at = DateTimeField(null=True)
    pending_action = CharField(max_length=32, null=True, default="")

    current_period_start = DateTimeField(null=True)
    current_period_end = DateTimeField(null=True)
    cancel_at_period_end = BooleanField(default=False)
    status = CharField(max_length=64, null=False, default="", index=True)

    class Meta:
        db_table = "billing_storage_subscription"



# -----------------------------------------------------------------------------
# Deprecated: UsageBased model (legacy table)
# -----------------------------------------------------------------------------
# The app currently uses:
# - `billing_payment_order` as the per-purchase ledger/history, and
# - `billing_purchased_product_overview` as the remaining quota snapshot.
#
# We keep the legacy schema commented out for potential future extension (e.g.,
# a dedicated usage-based purchase history table). If re-enabled, revisit the
# unique constraints (tenant_id/customer_id were unique) and make webhook
# handling idempotent.
#
# class UsageBased(DataBaseModel):
#     """
#     A tenant can purchase many usage-based product
#     """
#
#     id = CharField(max_length=32, primary_key=True)
#     tenant_id = CharField(max_length=32, null=False, index=True, unique=True)
#     product_id = CharField(max_length=32, null=False)
#     product_name = CharField(max_length=255, null=False, help_text="usage-based product name", index=True)
#     quantity = IntegerField(null=False)
#     order_id = CharField(max_length=128, null=False, help_text="from RAGFlow Order")
#     status = CharField(choices=[item.value for item in UsageBasedStatus])
#
#     # stripe
#     customer_id = CharField(max_length=255, null=False, default="", help_text="customer id on stripe.com", index=True, unique=True)
#     price_id = CharField(max_length=128, null=False, help_text="stripe subscription price_id")
#     payment_id = CharField(max_length=255, null=False, default="", help_text="checkout id on stripe.com", index=True)
#     payment_status = CharField(null=False, choices=[item.value for item in PaymentStatus], help_text="Our payment status", default=PaymentStatus.PENDING.value, index=True)
#     stripe_status = CharField(max_length=255, null=False, default="", help_text="raw status from stripe", index=True)
#
#     class Meta:
#         db_table = "billing_usage_based"


class PointAccount(DataBaseModel):
    """Per-tenant point balance account."""

    id = CharField(max_length=32, primary_key=True)
    tenant_id = CharField(max_length=32, null=False, index=True, unique=True)
    consumed_plan_points = BigIntegerField(null=False, default=0)  # consumed from plan quota (resets each cycle)
    addon_purchased_points = BigIntegerField(null=False, default=0)  # total addon points purchased (permanent)
    consumed_addon_points = BigIntegerField(null=False, default=0)  # consumed from addon purchased
    held_points = BigIntegerField(null=False, default=0)  # points held pending commit

    class Meta:
        db_table = "billing_point_account"


class PointLedger(DataBaseModel):
    """Immutable event stream for all point movements."""

    id = CharField(max_length=32, primary_key=True)
    tenant_id = CharField(max_length=32, null=False, index=True)
    event_type = CharField(max_length=32, null=False)
    # event_type choices: recharge / hold_created / consume / release
    points = BigIntegerField(null=False)  # positive=credit, negative=debit
    source = CharField(max_length=16, null=False, default="plan")  # "addon" or "plan"
    idempotency_key = CharField(max_length=128, null=False, unique=True, index=True)
    related_hold_id = CharField(max_length=32, null=True, index=True)
    description = TextField(null=True)
    metadata = JSONField(null=True, default={})

    class Meta:
        db_table = "billing_point_ledger"


class PointHold(DataBaseModel):
    """Represents a point reservation for an in-progress document parse."""

    id = CharField(max_length=32, primary_key=True)
    tenant_id = CharField(max_length=32, null=False, index=True)
    doc_id = CharField(max_length=32, null=False, index=True)
    points = BigIntegerField(null=False)
    plan_points = BigIntegerField(null=False, default=0)  # portion deducted from plan quota
    addon_points = BigIntegerField(null=False, default=0)  # portion deducted from addon
    status = CharField(max_length=32, null=False, default="held", index=True)
    # status choices: held / committed / released / expired
    idempotency_key = CharField(max_length=128, null=False, unique=True, index=True)
    expired_at = DateTimeField(null=True)

    class Meta:
        db_table = "billing_point_hold"


class UserCanvasVersion(DataBaseModel):
    id = CharField(max_length=32, primary_key=True)
    user_canvas_id = CharField(max_length=255, null=False, help_text="user_canvas_id", index=True)

    title = CharField(max_length=255, null=True, help_text="Canvas title")
    description = TextField(null=True, help_text="Canvas description")
    release = BooleanField(null=False, help_text="is released", default=False, index=True)
    dsl = JSONField(null=True, default={})

    class Meta:
        db_table = "user_canvas_version"


class MCPServer(DataBaseModel):
    id = CharField(max_length=32, primary_key=True)
    name = CharField(max_length=255, null=False, help_text="MCP Server name")
    tenant_id = CharField(max_length=32, null=False, index=True)
    url = CharField(max_length=2048, null=False, help_text="MCP Server URL")
    server_type = CharField(max_length=32, null=False, help_text="MCP Server type")
    description = TextField(null=True, help_text="MCP Server description")
    variables = JSONField(null=True, default=dict, help_text="MCP Server variables")
    headers = JSONField(null=True, default=dict, help_text="MCP Server additional request headers")

    class Meta:
        db_table = "mcp_server"


class Search(DataBaseModel):
    id = CharField(max_length=32, primary_key=True)
    avatar = TextField(null=True, help_text="avatar base64 string")
    tenant_id = CharField(max_length=32, null=False, index=True)
    name = CharField(max_length=128, null=False, help_text="Search name", index=True)
    description = TextField(null=True, help_text="KB description")
    created_by = CharField(max_length=32, null=False, index=True)
    search_config = JSONField(
        null=False,
        default={
            "kb_ids": [],
            "doc_ids": [],
            "similarity_threshold": 0.2,
            "vector_similarity_weight": 0.3,
            "use_kg": False,
            # rerank settings
            "rerank_id": "",
            "top_k": 1024,
            # chat settings
            "summary": False,
            "chat_id": "",
            # Leave it here for reference, don't need to set default values
            "llm_setting": {
                # "temperature": 0.1,
                # "top_p": 0.3,
                # "frequency_penalty": 0.7,
                # "presence_penalty": 0.4,
            },
            "chat_settingcross_languages": [],
            "highlight": False,
            "keyword": False,
            "web_search": False,
            "related_search": False,
            "query_mindmap": False,
        },
    )
    status = CharField(max_length=1, null=True, help_text="is it validate(0: wasted, 1: validate)", default="1", index=True)

    def __str__(self):
        return self.name

    class Meta:
        db_table = "search"


class PipelineOperationLog(DataBaseModel):
    id = CharField(max_length=32, primary_key=True)
    document_id = CharField(max_length=32, index=True)
    tenant_id = CharField(max_length=32, null=False, index=True)
    kb_id = CharField(max_length=32, null=False, index=True)
    pipeline_id = CharField(max_length=32, null=True, help_text="Pipeline ID", index=True)
    pipeline_title = CharField(max_length=32, null=True, help_text="Pipeline title", index=True)
    parser_id = CharField(max_length=32, null=False, help_text="Parser ID", index=True)
    document_name = CharField(max_length=255, null=False, help_text="File name")
    document_suffix = CharField(max_length=255, null=False, help_text="File suffix")
    document_type = CharField(max_length=255, null=False, help_text="Document type")
    source_from = CharField(max_length=255, null=False, help_text="Source")
    progress = FloatField(default=0, index=True)
    progress_msg = TextField(null=True, help_text="process message", default="")
    process_begin_at = DateTimeField(null=True, index=True)
    process_duration = FloatField(default=0)
    dsl = JSONField(null=True, default=dict)
    task_type = CharField(max_length=32, null=False, default="")
    operation_status = CharField(max_length=32, null=False, help_text="Operation status")
    avatar = TextField(null=True, help_text="avatar base64 string")
    status = CharField(max_length=1, null=True, help_text="is it validate(0: wasted, 1: validate)", default="1", index=True)

    class Meta:
        db_table = "pipeline_operation_log"


class WhiteList(DataBaseModel):
    id = PrimaryKeyField()
    email = CharField(max_length=255, null=False, index=True)

    class Meta:
        db_table = "white_list"


class Connector(DataBaseModel):
    id = CharField(max_length=32, primary_key=True)
    tenant_id = CharField(max_length=32, null=False, index=True)
    name = CharField(max_length=128, null=False, help_text="Search name", index=False)
    source = CharField(max_length=128, null=False, help_text="Data source", index=True)
    input_type = CharField(max_length=128, null=False, help_text="poll/event/..", index=True)
    config = JSONField(null=False, default={})
    refresh_freq = IntegerField(default=0, index=False)
    prune_freq = IntegerField(default=0, index=False)
    timeout_secs = IntegerField(default=3600, index=False)
    indexing_start = DateTimeField(null=True, index=True)
    status = CharField(max_length=16, null=True, help_text="schedule", default="schedule", index=True)

    def __str__(self):
        return self.name

    class Meta:
        db_table = "connector"


class Connector2Kb(DataBaseModel):
    id = CharField(max_length=32, primary_key=True)
    connector_id = CharField(max_length=32, null=False, index=True)
    kb_id = CharField(max_length=32, null=False, index=True)
    auto_parse = CharField(max_length=1, null=False, default="1", index=False)
    is_kb = CharField(max_length=1, null=True, help_text="is it to kb(0: no, 1: yes)", default="1", index=True)

    class Meta:
        db_table = "connector2kb"


class DateTimeTzField(CharField):
    field_type = 'VARCHAR'

    def db_value(self, value: datetime|None) -> str|None:
        if value is not None:
            if value.tzinfo is not None:
                return value.isoformat()
            else:
                return value.replace(tzinfo=timezone.utc).isoformat()
        return value

    def python_value(self, value: str|None) -> datetime|None:
        if value is not None:
            dt = datetime.fromisoformat(value)
            if dt.tzinfo is None:
                import pytz
                return dt.replace(tzinfo=pytz.UTC)
            return dt
        return value


class SyncLogs(DataBaseModel):
    id = CharField(max_length=32, primary_key=True)
    connector_id = CharField(max_length=32, index=True)
    status = CharField(max_length=128, null=False, help_text="Processing status", index=True)
    from_beginning = CharField(max_length=1, null=True, help_text="", default="0", index=False)
    new_docs_indexed = IntegerField(default=0, index=False)
    total_docs_indexed = IntegerField(default=0, index=False)
    docs_removed_from_index = IntegerField(default=0, index=False)
    error_msg = TextField(null=False, help_text="process message", default="")
    error_count = IntegerField(default=0, index=False)
    full_exception_trace = TextField(null=True, help_text="process message", default="")
    time_started = DateTimeField(null=True, index=True)
    poll_range_start = DateTimeTzField(max_length=255, null=True, index=True)
    poll_range_end = DateTimeTzField(max_length=255, null=True, index=True)
    kb_id = CharField(max_length=32, null=False, index=True)

    class Meta:
        db_table = "sync_logs"


class EvaluationCollection(DataBaseModel):
    """Ground truth collection for RAG evaluation"""
    id = CharField(max_length=32, primary_key=True)
    tenant_id = CharField(max_length=32, null=False, index=True, help_text="tenant ID")
    target_type = CharField(max_length=16, null=False, default="chat", index=True, help_text="chat|agent")
    name = CharField(max_length=255, null=False, index=True, help_text="collection name")
    description = TextField(null=True, help_text="collection description")
    created_by = CharField(max_length=32, null=False, index=True, help_text="creator user ID")
    status = IntegerField(null=False, default=1, help_text="1=valid, 0=invalid")

    class Meta:
        db_table = "evaluation_collections"


class EvaluationCase(DataBaseModel):
    """Individual test case in an evaluation collection"""
    id = CharField(max_length=32, primary_key=True)
    collection_id = CharField(max_length=32, null=False, index=True, help_text="FK to evaluation_collections")
    variable = JSONField(null=False, help_text="test question and answer", default={})
    relevant_doc_ids = JSONField(null=True, help_text="expected relevant document IDs", default=[])
    relevant_kb_ids = JSONField(null=True, help_text="expected relevant knowledge base IDs", default=[])
    metadata = JSONField(null=True, help_text="additional context/tags", default={})

    class Meta:
        db_table = "evaluation_cases"


class EvaluationRun(DataBaseModel):
    """A single evaluation run"""
    id = CharField(max_length=32, primary_key=True)
    collection_id = CharField(max_length=32, null=False, index=True, help_text="FK to evaluation_collections")
    target_type = CharField(max_length=32, null=False, index=True, help_text="chat|agent", default="chat")
    target_id = CharField(max_length=32, null=False, index=True, help_text="target object id", default="")
    name = CharField(max_length=255, null=False, help_text="run name")
    config_snapshot = JSONField(null=False, help_text="dialog config at time of evaluation")
    metrics_summary = JSONField(null=True, help_text="aggregated metrics")
    status = CharField(max_length=32, null=False, default="PENDING", help_text="PENDING/RUNNING/COMPLETED/FAILED")
    created_by = CharField(max_length=32, null=False, index=True, help_text="user who started the run")
    complete_time = BigIntegerField(null=True, help_text="completion timestamp")
    task_id = CharField(max_length=32, null=True, help_text="FK to evaluation_collections")

    class Meta:
        db_table = "evaluation_runs"


class EvaluationResult(DataBaseModel):
    """Result for a single test case in an evaluation run"""
    id = CharField(max_length=32, primary_key=True)
    run_id = CharField(max_length=32, null=False, index=True, help_text="FK to evaluation_runs")
    case_id = CharField(max_length=32, null=False, index=True, help_text="FK to evaluation_cases")
    generated_answer = TextField(null=False, help_text="generated answer")
    retrieved_chunks = JSONField(null=False, help_text="chunks that were retrieved")
    metrics = JSONField(null=False, help_text="all computed metrics")
    execution_time = FloatField(null=False, help_text="response time in seconds")
    token_usage = JSONField(null=True, help_text="prompt/completion tokens")

    class Meta:
        db_table = "evaluation_results"


class Memory(DataBaseModel):
    id = CharField(max_length=32, primary_key=True)
    name = CharField(max_length=128, null=False, index=False, help_text="Memory name")
    avatar = TextField(null=True, help_text="avatar base64 string")
    tenant_id = CharField(max_length=32, null=False, index=True)
    memory_type = IntegerField(null=False, default=1, index=True, help_text="Bit flags (LSB->MSB): 1=raw, 2=semantic, 4=episodic, 8=procedural. E.g., 5 enables raw + episodic.")
    storage_type = CharField(max_length=32, default='table', null=False, index=True, help_text="table|graph")
    embd_id = CharField(max_length=128, null=False, index=False, help_text="embedding model ID")
    tenant_embd_id = IntegerField(null=True, help_text="id in tenant_llm", index=True)
    llm_id = CharField(max_length=128, null=False, index=False, help_text="chat model ID")
    tenant_llm_id = IntegerField(null=True, help_text="id in tenant_llm", index=True)
    permissions = CharField(max_length=16, null=False, index=True, help_text="me|team", default="me")
    description = TextField(null=True, help_text="description")
    memory_size = IntegerField(default=5242880, null=False, index=False)
    forgetting_policy = CharField(max_length=32, null=False, default="FIFO", index=False, help_text="LRU|FIFO")
    temperature = FloatField(default=0.5, index=False)
    system_prompt = TextField(null=True, help_text="system prompt", index=False)
    user_prompt = TextField(null=True, help_text="user prompt", index=False)

    class Meta:
        db_table = "memory"

class SystemSettings(DataBaseModel):
    name = CharField(max_length=128, primary_key=True)
    source = CharField(max_length=32, null=False, index=False)
    data_type = CharField(max_length=32, null=False, index=False)
    value = TextField(null=False, help_text="Configuration value (JSON, string, etc.)")
    class Meta:
        db_table = "system_settings"

def alter_db_add_column(migrator, table_name, column_name, column_type):
    try:
        migrate(migrator.add_column(table_name, column_name, column_type))
    except OperationalError as ex:
        error_codes = [1060]
        error_messages = ['Duplicate column name']

        should_skip_error = (
                (hasattr(ex, 'args') and ex.args and ex.args[0] in error_codes) or
                (str(ex) in error_messages)
        )

        if not should_skip_error:
            logging.critical(f"Failed to add {settings.DATABASE_TYPE.upper()}.{table_name} column {column_name}, operation error: {ex}")

    except Exception as ex:
        logging.critical(f"Failed to add {settings.DATABASE_TYPE.upper()}.{table_name} column {column_name}, error: {ex}")
        pass

def alter_db_column_type(migrator, table_name, column_name, new_column_type):
    try:
        migrate(migrator.alter_column_type(table_name, column_name, new_column_type))
    except Exception as ex:
        logging.critical(f"Failed to alter {settings.DATABASE_TYPE.upper()}.{table_name} column {column_name} type, error: {ex}")
        pass

def alter_db_rename_column(migrator, table_name, old_column_name, new_column_name):
    try:
        migrate(migrator.rename_column(table_name, old_column_name, new_column_name))
    except Exception:
        # rename fail will lead to a weired error.
        # logging.critical(f"Failed to rename {settings.DATABASE_TYPE.upper()}.{table_name} column {old_column_name} to {new_column_name}, error: {ex}")
        pass

def alter_db_rename_table(migrator, old_table_name, new_table_name):
    try:
        migrate(migrator.rename_table(old_table_name, new_table_name))
    except Exception:
        # rename fail will lead to a weired error.
        # logging.critical(f"Failed to rename {settings.DATABASE_TYPE.upper()}.{old_table_name} table to {new_table_name}, error: {ex}")
        pass

def alter_db_remove_column(migrator, table_name, column_name):
    try:
        migrate(migrator.drop_column(table_name, column_name))
    except OperationalError as ex:
        error_codes = [1091]
        error_messages = ["Check that column/key exists", "doesn't exist", "does not exist"]

        should_skip_error = (
                (hasattr(ex, 'args') and ex.args and ex.args[0] in error_codes) or
                any(message in str(ex) for message in error_messages)
        )

        if not should_skip_error:
            logging.critical(f"Failed to drop {settings.DATABASE_TYPE.upper()}.{table_name} column {column_name}, operation error: {ex}")
    except Exception as ex:
        logging.critical(f"Failed to drop {settings.DATABASE_TYPE.upper()}.{table_name} column {column_name}, error: {ex}")
        pass

def migrate_add_unique_email(migrator):
    """Deduplicates user emails and add UNIQUE constraint to email column (idempotent)"""
    # step 0: check existing index state on user.email and prepare for unique constraint
    try:
        if settings.DATABASE_TYPE.upper() == "POSTGRES":
            cursor = DB.execute_sql("""
                SELECT COUNT(*)
                FROM pg_indexes
                WHERE tablename = 'user'
                  AND indexname = 'user_email'
            """)
            result = cursor.fetchone()
            if result and result[0] > 0:
                logging.info("UNIQUE index on user.email already exists, skipping migration")
                return
        else:
            # Fetch the first index on email: tells us both the name and whether it's unique.
            # non_unique=0 means unique, non_unique=1 means non-unique.
            cursor = DB.execute_sql("""
                SELECT index_name, non_unique
                FROM information_schema.statistics
                WHERE table_schema = DATABASE()
                  AND table_name = 'user'
                  AND column_name = 'email'
                LIMIT 1
            """)
            row = cursor.fetchone()
            if row:
                index_name, non_unique = row
                if non_unique == 0:
                    logging.info("UNIQUE index on user.email already exists, skipping migration")
                    return
                # Non-unique index exists (e.g. from old peewee index=True); drop it so
                # the upcoming ADD UNIQUE INDEX does not hit MySQL error 1061 "Duplicate key name".
                DB.execute_sql(f"ALTER TABLE `user` DROP INDEX `{index_name}`")
                logging.info(f"Dropped non-unique index '{index_name}' on user.email before adding unique index")
    except Exception as ex:
        logging.warning(f"Failed to check/prepare email index on user table: {ex}, continuing with migration")

    # step 1: rename duplicate rows so the UNIQUE constraint can be applied
    try:
        duplicates = User.select(User.email).group_by(User.email).having(fn.COUNT(User.id) > 1).tuples()
        for (dup_email,) in duplicates:
            # Keep the superuser row, or the oldest row if there is no superuser
            rows = list(
                User
                    .select(User.id)
                    .where(User.email == dup_email)
                    .order_by(User.is_superuser.desc(), User.create_time.asc())
                    .tuples()
            )
            for (uid,) in rows[1:]:
                new_email = f"{dup_email}_DUPLICATE_{uid[:8]}"
                User.update(email=new_email).where(User.id == uid).execute()
                logging.warning("Renamed duplicate user %s email to %s during migration", uid, new_email)
    except Exception as ex:
        logging.critical("Failed to deduplicate user.email before adding UNIQUE constraint: %s", ex)
        return

    # step 2: add UNIQUE index via migrator
    try:
        migrate(migrator.add_index("user", ("email",), unique=True))
    except (OperationalError, ProgrammingError) as ex:
        msg = str(ex)
        # MySQL 1061 "Duplicate key name" or PostgreSQL "already exists" -> already migrated
        if "1061" in msg or "Duplicate key name" in msg or "already exists" in msg.lower():
            pass
        else:
            logging.critical("Failed to add UNIQUE constraint on user.email: %s", ex)
    except Exception as ex:
        logging.critical("Failed to add UNIQUE constraint on user.email: %s", ex)



def update_tenant_llm_to_id_primary_key():
    """Add ID and set to primary key step by step."""
    if settings.DATABASE_TYPE.upper() == "POSTGRES":
        _update_tenant_llm_to_id_primary_key_postgres()
    else:
        _update_tenant_llm_to_id_primary_key_mysql()


def _update_tenant_llm_to_id_primary_key_mysql():
    """MySQL implementation: Add ID column and set as AUTO_INCREMENT primary key."""
    try:
        with DB.atomic():
            # 0. Check if 'id' column already exists
            cursor = DB.execute_sql("""
                            SELECT COLUMN_NAME
                            FROM INFORMATION_SCHEMA.COLUMNS
                            WHERE TABLE_SCHEMA = DATABASE()
                            AND TABLE_NAME = 'tenant_llm'
                            AND COLUMN_NAME = 'id'
                        """)
            if cursor.rowcount > 0:
                return

            # 1. Add nullable column
            DB.execute_sql("ALTER TABLE tenant_llm ADD COLUMN temp_id INT NULL")

            # 2. Set ID using MySQL user variables
            DB.execute_sql("SET @row = 0;")
            DB.execute_sql("UPDATE tenant_llm SET temp_id = (@row := @row + 1) ORDER BY tenant_id, llm_factory, llm_name;")

            # 3. Drop old primary key
            DB.execute_sql("ALTER TABLE tenant_llm DROP PRIMARY KEY")

            # 4. Update ID column to primary key with AUTO_INCREMENT
            DB.execute_sql("""
            ALTER TABLE tenant_llm
            MODIFY COLUMN temp_id INT NOT NULL AUTO_INCREMENT PRIMARY KEY
            """)

            # 5. Add unique key
            DB.execute_sql("""
                ALTER TABLE tenant_llm
                ADD CONSTRAINT uk_tenant_llm UNIQUE (tenant_id, llm_factory, llm_name)
            """)

            # 6. rename
            DB.execute_sql("ALTER TABLE tenant_llm RENAME COLUMN temp_id TO id")

            logging.info("Successfully updated tenant_llm to id primary key.")

    except Exception as e:
        logging.error(str(e))
        cursor = DB.execute_sql("""
                                    SELECT COLUMN_NAME
                                    FROM INFORMATION_SCHEMA.COLUMNS
                                    WHERE TABLE_SCHEMA = DATABASE()
                                    AND TABLE_NAME = 'tenant_llm'
                                    AND COLUMN_NAME = 'temp_id'
                                """)
        if cursor.rowcount > 0:
            DB.execute_sql("ALTER TABLE tenant_llm DROP COLUMN temp_id")


def _update_tenant_llm_to_id_primary_key_postgres():
    """PostgreSQL implementation: Add SERIAL primary key column to tenant_llm."""
    try:
        with DB.atomic():
            # 0. Check if 'id' column already exists
            cursor = DB.execute_sql("""
                            SELECT column_name
                            FROM information_schema.columns
                            WHERE table_catalog = current_database()
                            AND table_name = 'tenant_llm'
                            AND column_name = 'id'
                        """)
            if cursor.rowcount > 0:
                return

            # 1. Add nullable integer column
            DB.execute_sql("ALTER TABLE tenant_llm ADD COLUMN temp_id INTEGER NULL")

            # 2. Assign sequential row numbers ordered consistently
            DB.execute_sql("""
                UPDATE tenant_llm
                SET temp_id = subq.rn
                FROM (
                    SELECT ctid,
                           ROW_NUMBER() OVER (ORDER BY tenant_id, llm_factory, llm_name) AS rn
                    FROM tenant_llm
                ) AS subq
                WHERE tenant_llm.ctid = subq.ctid
            """)

            # 3. Drop old composite primary key constraint
            cursor = DB.execute_sql("""
                SELECT constraint_name
                FROM information_schema.table_constraints
                WHERE table_catalog = current_database()
                  AND table_name = 'tenant_llm'
                  AND constraint_type = 'PRIMARY KEY'
            """)
            row = cursor.fetchone()
            if row:
                DB.execute_sql(f'ALTER TABLE tenant_llm DROP CONSTRAINT "{row[0]}"')

            # 4. Make temp_id NOT NULL and create a sequence for it
            DB.execute_sql("ALTER TABLE tenant_llm ALTER COLUMN temp_id SET NOT NULL")
            DB.execute_sql("CREATE SEQUENCE IF NOT EXISTS tenant_llm_id_seq")
            DB.execute_sql("""
                SELECT setval('tenant_llm_id_seq', COALESCE((SELECT MAX(temp_id) FROM tenant_llm), 0))
            """)
            DB.execute_sql("ALTER TABLE tenant_llm ALTER COLUMN temp_id SET DEFAULT nextval('tenant_llm_id_seq')")
            DB.execute_sql("ALTER SEQUENCE tenant_llm_id_seq OWNED BY tenant_llm.temp_id")
            DB.execute_sql("ALTER TABLE tenant_llm ADD PRIMARY KEY (temp_id)")

            # 5. Add unique constraint
            DB.execute_sql("""
                ALTER TABLE tenant_llm
                ADD CONSTRAINT uk_tenant_llm UNIQUE (tenant_id, llm_factory, llm_name)
            """)

            # 6. Rename temp_id to id
            DB.execute_sql("ALTER TABLE tenant_llm RENAME COLUMN temp_id TO id")

            logging.info("Successfully updated tenant_llm to id primary key (PostgreSQL).")

    except Exception as e:
        logging.error(str(e))
        cursor = DB.execute_sql("""
                                    SELECT column_name
                                    FROM information_schema.columns
                                    WHERE table_catalog = current_database()
                                    AND table_name = 'tenant_llm'
                                    AND column_name = 'temp_id'
                                """)
        if cursor.rowcount > 0:
            DB.execute_sql("ALTER TABLE tenant_llm DROP COLUMN temp_id")


def migrate_db():
    logging.disable(logging.ERROR)

    migrator = DatabaseMigrator[settings.DATABASE_TYPE.upper()].value(DB)

    # from open source
    migrator = DatabaseMigrator[settings.DATABASE_TYPE.upper()].value(DB)
    alter_db_add_column(migrator, "file", "source_type", CharField(max_length=128, null=False, default="", help_text="where dose this document come from", index=True))
    alter_db_add_column(migrator, "tenant", "rerank_id", CharField(max_length=128, null=False, default="BAAI/bge-reranker-v2-m3", help_text="default rerank model ID"))
    alter_db_add_column(migrator, "dialog", "rerank_id", CharField(max_length=128, null=False, default="", help_text="default rerank model ID"))
    alter_db_column_type(migrator, "dialog", "top_k", IntegerField(default=1024))
    alter_db_add_column(migrator, "tenant_llm", "api_key", CharField(max_length=2048, null=True, help_text="API KEY", index=True))
    alter_db_add_column(migrator, "api_token", "source", CharField(max_length=16, null=True, help_text="none|agent|dialog", index=True))
    alter_db_add_column(migrator, "tenant", "tts_id", CharField(max_length=256, null=True, help_text="default tts model ID", index=True))
    alter_db_add_column(migrator, "api_4_conversation", "source", CharField(max_length=16, null=True, help_text="none|agent|dialog", index=True))
    alter_db_add_column(migrator, "task", "retry_count", IntegerField(default=0))
    alter_db_column_type(migrator, "api_token", "dialog_id", CharField(max_length=32, null=True, index=True))
    alter_db_add_column(migrator, "tenant_llm", "max_tokens", IntegerField(default=8192, index=True))
    alter_db_add_column(migrator, "api_4_conversation", "dsl", JSONField(null=True, default={}))
    alter_db_add_column(migrator, "knowledgebase", "pagerank", IntegerField(default=0, index=False))
    alter_db_add_column(migrator, "api_token", "beta", CharField(max_length=255, null=True, index=True))
    alter_db_add_column(migrator, "task", "digest", TextField(null=True, help_text="task digest", default=""))
    alter_db_add_column(migrator, "task", "chunk_ids", LongTextField(null=True, help_text="chunk ids", default=""))
    alter_db_add_column(migrator, "conversation", "user_id", CharField(max_length=255, null=True, help_text="user_id", index=True))
    alter_db_add_column(migrator, "task", "task_type", CharField(max_length=32, null=False, default=""))
    alter_db_add_column(migrator, "task", "priority", IntegerField(default=0))
    alter_db_add_column(migrator, "user_canvas", "permission", CharField(max_length=16, null=False, help_text="me|team", default="me", index=True))
    alter_db_add_column(migrator, "user_canvas", "release", BooleanField(null=False, help_text="is released", default=False, index=True))
    alter_db_add_column(migrator, "llm", "is_tools", BooleanField(null=False, help_text="support tools", default=False))
    alter_db_add_column(migrator, "mcp_server", "variables", JSONField(null=True, help_text="MCP Server variables", default=dict))
    alter_db_rename_column(migrator, "task", "process_duation", "process_duration")
    alter_db_rename_column(migrator, "document", "process_duation", "process_duration")
    alter_db_add_column(migrator, "document", "suffix", CharField(max_length=32, null=False, default="", help_text="The real file extension suffix", index=True))
    alter_db_add_column(migrator, "api_4_conversation", "errors", TextField(null=True, help_text="errors"))
    alter_db_add_column(migrator, "dialog", "meta_data_filter", JSONField(null=True, default={}))
    alter_db_column_type(migrator, "canvas_template", "title", JSONField(null=True, default=dict, help_text="Canvas title"))
    alter_db_column_type(migrator, "canvas_template", "description", JSONField(null=True, default=dict, help_text="Canvas description"))
    alter_db_add_column(migrator, "user_canvas", "canvas_category", CharField(max_length=32, null=False, default="agent_canvas", help_text="agent_canvas|dataflow_canvas", index=True))
    alter_db_add_column(migrator, "canvas_template", "canvas_category", CharField(max_length=32, null=False, default="agent_canvas", help_text="agent_canvas|dataflow_canvas", index=True))
    alter_db_add_column(migrator, "knowledgebase", "pipeline_id", CharField(max_length=32, null=True, help_text="Pipeline ID", index=True))
    alter_db_add_column(migrator, "document", "pipeline_id", CharField(max_length=32, null=True, help_text="Pipeline ID", index=True))
    alter_db_add_column(migrator, "knowledgebase", "graphrag_task_id", CharField(max_length=32, null=True, help_text="Gragh RAG task ID", index=True))
    alter_db_add_column(migrator, "knowledgebase", "raptor_task_id", CharField(max_length=32, null=True, help_text="RAPTOR task ID", index=True))
    alter_db_add_column(migrator, "knowledgebase", "graphrag_task_finish_at", DateTimeField(null=True))
    alter_db_add_column(migrator, "knowledgebase", "raptor_task_finish_at", CharField(null=True))
    alter_db_add_column(migrator, "knowledgebase", "mindmap_task_id", CharField(max_length=32, null=True, help_text="Mindmap task ID", index=True))
    alter_db_add_column(migrator, "knowledgebase", "mindmap_task_finish_at", CharField(null=True))
    alter_db_column_type(migrator, "tenant_llm", "api_key", TextField(null=True, help_text="API KEY"))
    alter_db_add_column(migrator, "tenant_llm", "status", CharField(max_length=1, null=False, help_text="is it validate(0: wasted, 1: validate)", default="1", index=True))
    alter_db_add_column(migrator, "connector2kb", "auto_parse", CharField(max_length=1, null=False, default="1", index=False))
    alter_db_add_column(migrator, "connector2kb", "is_kb", CharField(max_length=1, null=True, help_text="is it to kb(0: no, 1: yes)", default="1", index=True))
    alter_db_add_column(migrator, "llm_factories", "rank", IntegerField(default=0, index=False))

    # for EE
    alter_db_add_column(migrator, "user", "role_id", IntegerField(null=False, help_text="id in rag_flow.role", index=True, default=1))
    alter_db_add_column(migrator, "knowledgebase", "embed_task_id", CharField(max_length=32, null=True, help_text="Switch embedding task ID", index=True))
    alter_db_add_column(migrator, "knowledgebase", "embed_task_finish_at", DateTimeField(null=True))
    alter_db_add_column(migrator, "knowledgebase", "clone_task_id", CharField(max_length=32, null=True, help_text="Duplicate dataset task ID", index=True))
    alter_db_add_column(migrator, "knowledgebase", "clone_task_finish_at", DateTimeField(null=True))
    alter_db_add_column(migrator, "document", "from_kb_id", CharField(max_length=256, null=True, index=True))

    alter_db_rename_table(migrator, "evaluation_datasets", "evaluation_collections")
    alter_db_rename_column(migrator, "evaluation_cases", "dataset_id", "collection_id")
    alter_db_rename_column(migrator, "evaluation_runs", "dataset_id", "collection_id")
    alter_db_remove_column(migrator, "evaluation_collections", "kb_ids")
    alter_db_remove_column(migrator, "evaluation_cases", "question")
    alter_db_remove_column(migrator, "evaluation_cases", "reference_answer")
    alter_db_remove_column(migrator, "evaluation_cases", "relevant_doc_ids")
    alter_db_remove_column(migrator, "evaluation_cases", "relevant_chunk_ids")
    alter_db_remove_column(migrator, "evaluation_cases", "metadata")
    alter_db_remove_column(migrator, "evaluation_runs", "dialog_id")
    alter_db_add_column(migrator, "evaluation_cases", "variable", JSONField(null=False, help_text="test question and answer", default={}))
    alter_db_add_column(migrator, "evaluation_cases", "relevant_doc_ids", JSONField(null=True, help_text="expected relevant document IDs", default=[]))
    alter_db_add_column(migrator, "evaluation_cases", "relevant_kb_ids", JSONField(null=True, help_text="expected relevant knowledge base IDs", default=[]))
    alter_db_add_column(migrator, "evaluation_cases", "metadata", JSONField(null=True, help_text="additional context/tags", default={}))
    alter_db_add_column(migrator, "evaluation_runs", "target_type", CharField(max_length=32, null=False, index=True, help_text="chat|agent|...", default="chat"))
    alter_db_add_column(migrator, "evaluation_runs", "target_id", CharField(max_length=32, null=False, index=True, help_text="target object id", default=""))
    alter_db_add_column(migrator, "evaluation_collections", "target_type", CharField(max_length=16, null=False, default="chat", help_text="chat|agent", index=True))
    alter_db_add_column(migrator, "evaluation_runs", "task_id", CharField(max_length=32, null=True, help_text="task id"))

    # merge from open source (2026-03-09 13:05)
    alter_db_add_column(migrator, "api_4_conversation", "name", CharField(max_length=255, null=True, help_text="conversation name", index=False))
    alter_db_add_column(migrator, "api_4_conversation", "exp_user_id", CharField(max_length=255, null=True, help_text="exp_user_id", index=True))
    # Migrate system_settings.value from CharField to TextField for longer sandbox configs
    alter_db_column_type(migrator, "system_settings", "value", TextField(null=False, help_text="Configuration value (JSON, string, etc.)"))
    alter_db_add_column(migrator, "document", "content_hash", CharField(max_length=32, null=True, help_text="xxhash128 of document content for change detection", default="", index=True))
    update_tenant_llm_to_id_primary_key()
    alter_db_add_column(migrator, "tenant", "tenant_llm_id", IntegerField(null=True, help_text="id in tenant_llm", index=True))
    alter_db_add_column(migrator, "tenant", "tenant_embd_id", IntegerField(null=True, help_text="id in tenant_llm", index=True))
    alter_db_add_column(migrator, "tenant", "tenant_asr_id", IntegerField(null=True, help_text="id in tenant_llm", index=True))
    alter_db_add_column(migrator, "tenant", "tenant_img2txt_id", IntegerField(null=True, help_text="id in tenant_llm", index=True))
    alter_db_add_column(migrator, "tenant", "tenant_rerank_id", IntegerField(null=True, help_text="id in tenant_llm", index=True))
    alter_db_add_column(migrator, "tenant", "tenant_tts_id", IntegerField(null=True, help_text="id in tenant_llm", index=True))
    alter_db_add_column(migrator, "knowledgebase", "tenant_embd_id", IntegerField(null=True, help_text="id in tenant_llm", index=True))
    alter_db_add_column(migrator, "dialog", "tenant_llm_id", IntegerField(null=True, help_text="id in tenant_llm", index=True))
    alter_db_add_column(migrator, "dialog", "tenant_rerank_id", IntegerField(null=True, help_text="id in tenant_llm", index=True))
    alter_db_add_column(migrator, "memory", "tenant_embd_id", IntegerField(null=True, help_text="id in tenant_llm", index=True))
    alter_db_add_column(migrator, "memory", "tenant_llm_id", IntegerField(null=True, help_text="id in tenant_llm", index=True))
    alter_db_add_column(migrator, "user_canvas_version", "release", BooleanField(null=False, help_text="is released", default=False, index=True))
    alter_db_add_column(migrator, "api_4_conversation", "version_title", CharField(max_length=255, null=True, help_text="canvas version title when session created", index=False))

    # Billing points refactor: consumed-based tracking (2026-04-28)
    # PointAccount now tracks consumed amounts; available = quota - consumed
    alter_db_add_column(migrator, "billing_point_account", "consumed_plan_points", BigIntegerField(null=False, default=0))
    alter_db_add_column(migrator, "billing_point_account", "addon_purchased_points", BigIntegerField(null=False, default=0))
    alter_db_add_column(migrator, "billing_point_account", "consumed_addon_points", BigIntegerField(null=False, default=0))

    # MIGRATION NOTE (2026-04-28):
    # Existing accounts need consumed_plan_points and addon_purchased_points populated
    # from the ledger before the consumed-based model goes live. The migration requires
    # a three-pass approach (per tenant):
    #   1. addon_purchased_points = SUM(recharge where source=addon)
    #      (release events do NOT reduce purchased — release is hold cleanup, not a refund)
    #   2. consumed_plan_points  = SUM(consume where source=plan)
    #   3. consumed_addon_points = SUM(consume where source=addon)
    # After migration completes, available fields can be safely removed.
    # Add quota_points to billing_product (2026-04-28)
    alter_db_add_column(migrator, "billing_product", "quota_points", BigIntegerField(null=True))
    # This migration must be run as an offline batch job before billing_app starts serving
    # traffic with the new consumed-based model.
    alter_db_add_column(migrator, "billing_point_ledger", "source", CharField(max_length=16, null=False, default="plan"))
    alter_db_add_column(migrator, "billing_point_hold", "plan_points", BigIntegerField(null=False, default=0))
    alter_db_add_column(migrator, "billing_point_hold", "addon_points", BigIntegerField(null=False, default=0))
    alter_db_remove_column(migrator, "billing_point_account", "available_points")

    # Billing storage quantity columns migrate from legacy *_gb to *_bytes (int64)
    alter_db_rename_column(migrator, "billing_storage_subscription", "effective_quantity_gb", "addon_storage_bytes")
    alter_db_rename_column(migrator, "billing_storage_subscription", "addon_storage_gb", "addon_storage_bytes")
    alter_db_rename_column(migrator, "billing_storage_subscription", "target_quantity_gb", "target_quantity_bytes")
    alter_db_rename_column(migrator, "billing_storage_subscription", "pending_quantity_gb", "pending_quantity_bytes")
    alter_db_add_column(migrator, "billing_storage_subscription", "addon_storage_bytes", BigIntegerField(null=False, default=0))
    alter_db_add_column(migrator, "billing_storage_subscription", "target_quantity_bytes", BigIntegerField(null=False, default=0))
    alter_db_add_column(migrator, "billing_storage_subscription", "pending_quantity_bytes", BigIntegerField(null=True))
    alter_db_column_type(migrator, "billing_storage_subscription", "addon_storage_bytes", BigIntegerField(null=False, default=0))
    alter_db_column_type(migrator, "billing_storage_subscription", "target_quantity_bytes", BigIntegerField(null=False, default=0))
    alter_db_column_type(migrator, "billing_storage_subscription", "pending_quantity_bytes", BigIntegerField(null=True))

    # QuotaItem storage unit migration: convert legacy gb entries to bytes.
    alter_db_column_type(migrator, "billing_quota_item", "quantity", BigIntegerField(null=False))
    try:
        DB.execute_sql(
            """
            UPDATE billing_quota_item
            SET quantity = quantity * 1000000000,
                unit = 'bytes'
            WHERE quota_type = 'kb_storage' AND unit = 'gb'
            """
        )
    except Exception as ex:
        logging.critical(f"Failed to migrate billing_quota_item kb_storage gb->bytes, error: {ex}")

    logging.disable(logging.NOTSET)
    # this is after re-enabling logging to allow logging changed user emails
    migrate_add_unique_email(migrator)
