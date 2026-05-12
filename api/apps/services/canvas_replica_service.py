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

import json
import logging
import random
import time

from api.db import CanvasCategory, PermissionValue, ResourceType
from api.db.services.user_service import UserService, UserTenantService
from api.utils.permission_utils import has_permission_for_member
from agent.dsl_migration import normalize_chunker_dsl
from rag.utils.redis_conn import REDIS_CONN, RedisDistributedLock


class CanvasReplicaService:
    """
    Manage per-user canvas runtime replicas stored in Redis.

    Lifecycle:
    - bootstrap: initialize/refresh replica from DB DSL
    - load_for_run: read replica before run
    - commit_after_run: atomically persist run result back to replica
    """

    TTL_SECS = 3 * 60 * 60
    REPLICA_KEY_PREFIX = "canvas:replica"
    LOCK_KEY_PREFIX = "canvas:replica:lock"
    LOCK_TIMEOUT_SECS = 10
    LOCK_BLOCKING_TIMEOUT_SECS = 1
    LOCK_RETRY_ATTEMPTS = 3
    LOCK_RETRY_SLEEP_SECS = 0.2
    PRESENCE_TTL_SECS = 10
    PRESENCE_SESSION_KEY_PREFIX = "canvas:presence:session"
    PRESENCE_USER_KEY_PREFIX = "canvas:presence:user"
    PRESENCE_CANVAS_KEY_PREFIX = "canvas:presence:canvas"

    # Shared DSL normalization for DB snapshots, runtime replicas and SDK writes.
    @classmethod
    def normalize_dsl(cls, dsl):
        """Normalize DSL to a JSON-serializable dict. Raise ValueError on invalid input."""
        normalized = dsl
        if isinstance(normalized, str):
            try:
                normalized = json.loads(normalized)
            except Exception as e:
                raise ValueError("Invalid DSL JSON string.") from e

        if not isinstance(normalized, dict):
            raise ValueError("DSL must be a JSON object.")

        try:
            return json.loads(json.dumps(normalize_chunker_dsl(normalized), ensure_ascii=False))
        except Exception as e:
            raise ValueError("DSL is not JSON-serializable.") from e

    # Redis key helpers for runtime replica state.
    @classmethod
    def _replica_key(cls, canvas_id: str, tenant_id: str, runtime_user_id: str) -> str:
        return f"{cls.REPLICA_KEY_PREFIX}:{canvas_id}:{tenant_id}:{runtime_user_id}"


    @classmethod
    def _lock_key(cls, canvas_id: str, tenant_id: str, runtime_user_id: str) -> str:
        return f"{cls.LOCK_KEY_PREFIX}:{canvas_id}:{tenant_id}:{runtime_user_id}"

    # Redis key helpers for online presence state.
    @classmethod
    def _presence_session_key(cls, presence_session_id: str) -> str:
        return f"{cls.PRESENCE_SESSION_KEY_PREFIX}:{presence_session_id}"


    @classmethod
    def _presence_user_sessions_key(cls, canvas_id: str, tenant_id: str, runtime_user_id: str) -> str:
        return f"{cls.PRESENCE_USER_KEY_PREFIX}:{canvas_id}:{tenant_id}:{runtime_user_id}:sessions"


    @classmethod
    def _presence_canvas_users_key(cls, canvas_id: str) -> str:
        return f"{cls.PRESENCE_CANVAS_KEY_PREFIX}:{canvas_id}:users"


    @classmethod
    def _presence_canvas_user_ref(cls, tenant_id: str, runtime_user_id: str) -> str:
        return f"{tenant_id}:{runtime_user_id}"


    @classmethod
    def _parse_presence_canvas_user_ref(cls, user_ref: str) -> tuple[str | None, str | None]:
        tenant_id, sep, runtime_user_id = user_ref.partition(":")
        if not sep or not tenant_id or not runtime_user_id:
            return None, None
        return tenant_id, runtime_user_id


    # Presence payload helpers keep Redis session data small and uniform.
    @classmethod
    def _read_presence_payload(cls, presence_session_id: str):
        cache_blob = REDIS_CONN.get(cls._presence_session_key(presence_session_id))
        if not cache_blob:
            return None
        try:
            payload = json.loads(cache_blob)
        except Exception as e:
            logging.warning("Failed to parse presence session %s: %s", presence_session_id, e)
            return None
        if not isinstance(payload, dict):
            return None
        return payload


    @classmethod
    def _write_presence_payload(cls, payload: dict):
        presence_session_id = str(payload.get("presence_session_id", ""))
        if not presence_session_id:
            raise ValueError("presence_session_id is required.")
        REDIS_CONN.set_obj(
            cls._presence_session_key(presence_session_id),
            payload,
            cls.PRESENCE_TTL_SECS,
        )


    @classmethod
    def _build_presence_payload(
        cls,
        canvas_id: str,
        tenant_id: str,
        runtime_user_id: str,
        tab_id: str,
        presence_session_id: str,
        now: int | None = None,
    ):
        ts = int(now or time.time())
        return {
            "canvas_id": str(canvas_id),
            "tenant_id": str(tenant_id),
            "runtime_user_id": str(runtime_user_id),
            "user_id": str(runtime_user_id),
            "tab_id": str(tab_id),
            "presence_session_id": str(presence_session_id),
            "joined_at": ts,
            "last_seen_at": ts,
        }


    @classmethod
    def _cleanup_user_presence_sessions(cls, canvas_id: str, tenant_id: str, runtime_user_id: str):
        user_sessions_key = cls._presence_user_sessions_key(canvas_id, tenant_id, runtime_user_id)
        canvas_users_key = cls._presence_canvas_users_key(canvas_id)
        user_ref = cls._presence_canvas_user_ref(tenant_id, runtime_user_id)
        session_ids = list(REDIS_CONN.smembers(user_sessions_key) or [])
        active_sessions = []

        for presence_session_id in session_ids:
            payload = cls._read_presence_payload(presence_session_id)
            if not payload:
                REDIS_CONN.srem(user_sessions_key, presence_session_id)
                continue

            if (
                str(payload.get("canvas_id")) != str(canvas_id)
                or str(payload.get("tenant_id")) != str(tenant_id)
                or str(payload.get("runtime_user_id")) != str(runtime_user_id)
            ):
                REDIS_CONN.srem(user_sessions_key, presence_session_id)
                continue

            active_sessions.append(payload)

        if active_sessions:
            REDIS_CONN.sadd(canvas_users_key, user_ref)
        else:
            REDIS_CONN.srem(canvas_users_key, user_ref)
            REDIS_CONN.delete(user_sessions_key)

        return active_sessions


    # Presence responses show both readable names and effective canvas permissions.
    @classmethod
    def _get_presence_display_names(cls, user_ids: list[str]) -> dict[str, str]:
        display_names = {user_id: user_id for user_id in user_ids}
        if not user_ids:
            return display_names

        try:
            for user in UserService.query(id=user_ids):
                display_names[str(user.id)] = str(getattr(user, "nickname", "") or user.id)
        except Exception:
            logging.exception("Failed to load user nicknames for canvas presence.")

        return display_names


    @classmethod
    def _get_presence_permissions(cls, canvas_id: str, tenant_id: str, user_ids: list[str]) -> dict[str, int]:
        permissions = {}
        if not user_ids:
            return permissions

        tenant_member_memo = {}
        for user_id in user_ids:
            user_id = str(user_id)
            if user_id == str(tenant_id):
                permissions[user_id] = PermissionValue.PERMISSION_OWNER.value
                continue

            member = tenant_member_memo.get(user_id)
            if member is None:
                member = UserTenantService.filter_by_tenant_and_user_id(tenant_id, user_id)
                tenant_member_memo[user_id] = member

            if not member:
                permissions[user_id] = PermissionValue.PERMISSION_NULL.value
                continue

            permission_info = has_permission_for_member(
                operator_id=member.id,
                tenant_id=tenant_id,
                resource_id=canvas_id,
                resource_type=ResourceType.CANVAS,
                permission=PermissionValue.PERMISSION_READ,
            )
            permissions[user_id] = permission_info[2]

        return permissions


    # Runtime replica payload helpers.
    @classmethod
    def _read_payload(cls, replica_key: str):
        """Read replica payload from Redis; return None on missing/invalid content."""
        cache_blob = REDIS_CONN.get(replica_key)
        if not cache_blob:
            return None
        try:
            payload = json.loads(cache_blob)
            if not isinstance(payload, dict):
                return None
            payload["dsl"] = cls.normalize_dsl(payload.get("dsl", {}))
            return payload
        except Exception as e:
            logging.warning("Failed to parse canvas replica %s: %s", replica_key, e)
            return None


    @classmethod
    def _write_payload(cls, replica_key: str, payload: dict):
        """Write payload and refresh TTL."""
        payload["updated_at"] = int(time.time())
        REDIS_CONN.set_obj(replica_key, payload, cls.TTL_SECS)


    @classmethod
    def _build_payload(
        cls,
        canvas_id: str,
        tenant_id: str,
        runtime_user_id: str,
        dsl,
        canvas_category=CanvasCategory.Agent,
        title="",
    ):
        return {
            "canvas_id": canvas_id,
            "tenant_id": str(tenant_id),
            "runtime_user_id": str(runtime_user_id),
            "title": title or "",
            "canvas_category": canvas_category or CanvasCategory.Agent,
            "dsl": cls.normalize_dsl(dsl),
            "updated_at": int(time.time()),
        }


    # Runtime replica lifecycle used by get(), run() and save().
    @classmethod
    def create_if_absent(
        cls,
        canvas_id: str,
        tenant_id: str,
        runtime_user_id: str,
        dsl,
        canvas_category=CanvasCategory.Agent,
        title="",
    ):
        """Create a runtime replica if it does not exist; otherwise keep existing state."""
        replica_key = cls._replica_key(canvas_id, str(tenant_id), str(runtime_user_id))
        payload = cls._read_payload(replica_key)
        if payload:
            return payload
        payload = cls._build_payload(canvas_id, str(tenant_id), str(runtime_user_id), dsl, canvas_category, title)
        cls._write_payload(replica_key, payload)
        return payload


    @classmethod
    def bootstrap(
        cls,
        canvas_id: str,
        tenant_id: str,
        runtime_user_id: str,
        dsl,
        canvas_category=CanvasCategory.Agent,
        title="",
    ):
        """Bootstrap replica by creating it when absent and keeping existing runtime state."""
        return cls.create_if_absent(
            canvas_id=canvas_id,
            tenant_id=tenant_id,
            runtime_user_id=runtime_user_id,
            dsl=dsl,
            canvas_category=canvas_category,
            title=title,
        )


    @classmethod
    def load_for_run(cls, canvas_id: str, tenant_id: str, runtime_user_id: str):
        """Load current runtime replica used by /completions."""
        replica_key = cls._replica_key(canvas_id, str(tenant_id), str(runtime_user_id))
        return cls._read_payload(replica_key)


    @classmethod
    def replace_for_set(
        cls,
        canvas_id: str,
        tenant_id: str,
        runtime_user_id: str,
        dsl,
        canvas_category=CanvasCategory.Agent,
        title="",
    ):
        """Replace replica content for `/set` under lock."""
        replica_key = cls._replica_key(canvas_id, str(tenant_id), str(runtime_user_id))
        lock_key = cls._lock_key(canvas_id, str(tenant_id), str(runtime_user_id))
        lock = cls._acquire_lock_with_retry(lock_key)
        if not lock:
            logging.error("Failed to acquire canvas replica lock after retry: %s", lock_key)
            return False

        try:
            updated_payload = cls._build_payload(
                canvas_id=canvas_id,
                tenant_id=str(tenant_id),
                runtime_user_id=str(runtime_user_id),
                dsl=dsl,
                canvas_category=canvas_category,
                title=title,
            )
            cls._write_payload(replica_key, updated_payload)
            return True
        except Exception:
            logging.exception("Failed to replace canvas replica from /set.")
            return False
        finally:
            try:
                lock.release()
            except Exception:
                logging.exception("Failed to release canvas replica lock: %s", lock_key)


    @classmethod
    def _acquire_lock_with_retry(cls, lock_key: str):
        """Acquire distributed lock with bounded retries; return lock object or None."""
        lock = RedisDistributedLock(
            lock_key,
            timeout=cls.LOCK_TIMEOUT_SECS,
            blocking_timeout=cls.LOCK_BLOCKING_TIMEOUT_SECS,
        )
        for idx in range(cls.LOCK_RETRY_ATTEMPTS):
            if lock.acquire():
                return lock
            if idx < cls.LOCK_RETRY_ATTEMPTS - 1:
                time.sleep(cls.LOCK_RETRY_SLEEP_SECS + random.uniform(0, 0.1))
        return None


    @classmethod
    def commit_after_run(
        cls,
        canvas_id: str,
        tenant_id: str,
        runtime_user_id: str,
        dsl,
        canvas_category=CanvasCategory.Agent,
        title="",
    ):
        """
        Commit post-run DSL into replica.

        Returns:
            bool: True on committed/saved, False on commit failure.
        """
        new_dsl = cls.normalize_dsl(dsl)
        replica_key = cls._replica_key(canvas_id, str(tenant_id), str(runtime_user_id))

        try:
            latest_payload = cls._read_payload(replica_key)

            # Always write latest runtime DSL back to Redis first.
            updated_payload = cls._build_payload(
                canvas_id=canvas_id,
                tenant_id=str(tenant_id),
                runtime_user_id=str(runtime_user_id),
                dsl=new_dsl,
                canvas_category=canvas_category if not latest_payload else (canvas_category or latest_payload.get("canvas_category", CanvasCategory.Agent)),
                title=title if not latest_payload else (title or latest_payload.get("title", "")),
            )
            cls._write_payload(replica_key, updated_payload)

            return True
        except Exception:
            logging.exception("Failed to commit canvas runtime replica.")
            return False


    # Presence lifecycle used by the canvas collaboration UI.
    @classmethod
    def join_presence(
        cls,
        canvas_id: str,
        tenant_id: str,
        runtime_user_id: str,
        tab_id: str,
        presence_session_id: str,
    ):
        """Register one browser tab as online for this canvas."""
        payload = cls._build_presence_payload(
            canvas_id=canvas_id,
            tenant_id=tenant_id,
            runtime_user_id=runtime_user_id,
            tab_id=tab_id,
            presence_session_id=presence_session_id,
        )
        REDIS_CONN.sadd(
            cls._presence_user_sessions_key(canvas_id, tenant_id, runtime_user_id),
            str(presence_session_id),
        )
        REDIS_CONN.sadd(
            cls._presence_canvas_users_key(canvas_id),
            cls._presence_canvas_user_ref(str(tenant_id), str(runtime_user_id)),
        )
        cls._write_presence_payload(payload)
        return {"ok": True}


    @classmethod
    def heartbeat_presence(
        cls,
        canvas_id: str,
        tenant_id: str,
        runtime_user_id: str,
        tab_id: str,
        presence_session_id: str,
    ):
        """Refresh a valid presence session and extend its TTL."""
        payload = cls._read_presence_payload(presence_session_id)
        if not payload:
            return {"ok": False, "code": "SESSION_NOT_FOUND"}

        if (
            str(payload.get("canvas_id")) != str(canvas_id)
            or str(payload.get("tenant_id")) != str(tenant_id)
            or str(payload.get("runtime_user_id")) != str(runtime_user_id)
            or str(payload.get("tab_id")) != str(tab_id)
        ):
            return {"ok": False, "code": "INVALID_SESSION"}

        payload["last_seen_at"] = int(time.time())
        cls._write_presence_payload(payload)
        REDIS_CONN.sadd(
            cls._presence_user_sessions_key(canvas_id, tenant_id, runtime_user_id),
            str(presence_session_id),
        )
        REDIS_CONN.sadd(
            cls._presence_canvas_users_key(canvas_id),
            cls._presence_canvas_user_ref(str(tenant_id), str(runtime_user_id)),
        )
        return {"ok": True}


    @classmethod
    def leave_presence(
        cls,
        canvas_id: str,
        tenant_id: str,
        runtime_user_id: str,
        tab_id: str,
        presence_session_id: str,
    ):
        """Remove one tab session and clean stale online state."""
        payload = cls._read_presence_payload(presence_session_id)
        if payload and (
            str(payload.get("canvas_id")) == str(canvas_id)
            and str(payload.get("tenant_id")) == str(tenant_id)
            and str(payload.get("runtime_user_id")) == str(runtime_user_id)
            and str(payload.get("tab_id")) == str(tab_id)
        ):
            REDIS_CONN.delete(cls._presence_session_key(presence_session_id))
            REDIS_CONN.srem(
                cls._presence_user_sessions_key(canvas_id, tenant_id, runtime_user_id),
                str(presence_session_id),
            )
        cls._cleanup_user_presence_sessions(canvas_id, tenant_id, runtime_user_id)
        return {"ok": True}


    @classmethod
    def list_presence(
        cls,
        canvas_id: str,
        tenant_id: str,
        operator_permission: int | None = None,
    ):
        """Return active users with tab counts and effective permissions."""
        canvas_users_key = cls._presence_canvas_users_key(canvas_id)
        user_refs = list(REDIS_CONN.smembers(canvas_users_key) or [])
        users = []

        for user_ref in user_refs:
            tenant_id, runtime_user_id = cls._parse_presence_canvas_user_ref(str(user_ref))
            if not tenant_id or not runtime_user_id:
                REDIS_CONN.srem(canvas_users_key, user_ref)
                continue

            active_sessions = cls._cleanup_user_presence_sessions(canvas_id, tenant_id, runtime_user_id)
            if not active_sessions:
                continue

            users.append(
                {
                    "user_id": str(runtime_user_id),
                    "display_name": str(runtime_user_id),
                    "active_tab_count": len(active_sessions),
                    "last_seen_at": max(int(session.get("last_seen_at", 0) or 0) for session in active_sessions),
                    "permission": PermissionValue.PERMISSION_NULL.value,
                }
            )

        display_names = cls._get_presence_display_names([str(user["user_id"]) for user in users])
        permissions = cls._get_presence_permissions(
            canvas_id=canvas_id,
            tenant_id=tenant_id,
            user_ids=[str(user["user_id"]) for user in users],
        )
        for user in users:
            user["display_name"] = display_names.get(str(user["user_id"]), str(user["user_id"]))
            user["permission"] = permissions.get(
                str(user["user_id"]),
                PermissionValue.PERMISSION_NULL.value,
            )

        users.sort(key=lambda user: (-int(user["last_seen_at"]), str(user["user_id"])))

        return {
            "canvas_id": str(canvas_id),
            "online_user_count": len(users),
            "operator_permission": (
                operator_permission
                if operator_permission is not None
                else permissions.get(str(tenant_id), PermissionValue.PERMISSION_NULL.value)
            ),
            "users": users,
        }
