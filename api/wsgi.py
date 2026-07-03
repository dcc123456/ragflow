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

from common.config_utils import show_configs
from common.file_utils import get_project_base_directory
from common.log_utils import init_root_logger
from common.mcp_tool_call_conn import shutdown_all_mcp_sessions
from common.settings import print_rag_settings

import sys
import time

# Initialize logging first
init_root_logger("ragflow_server")
from plugin import GlobalPluginManager
import logging
import os
import signal
import threading
import uuid
from common import settings
from api.apps import app
from api.db.runtime_config import RuntimeConfig
from api.db.services.document_service import DocumentService
from api.db.db_models import init_database_tables as init_web_db
from api.db.init_data import init_web_data
from rag.utils.redis_conn import RedisDistributedLock

# Global stop event and executor for background tasks
stop_event = threading.Event()
background_executor = None

RAGFLOW_DEBUGPY_LISTEN = int(os.environ.get("RAGFLOW_DEBUGPY_LISTEN", "0"))


def update_progress():
    lock_value = str(uuid.uuid4())
    redis_lock = RedisDistributedLock("update_progress", lock_value=lock_value, timeout=60)
    logging.info(f"update_progress lock_value: {lock_value}")
    while not stop_event.is_set():
        try:
            if redis_lock.acquire():
                DocumentService.update_progress()
                redis_lock.release()
        except Exception:
            logging.exception("update_progress exception")
        finally:
            try:
                redis_lock.release()
            except Exception:
                logging.exception("update_progress exception")
            stop_event.wait(6)
            time.sleep(3)


def signal_handler(sig, frame):
    logging.info("Received interrupt signal, shutting down...")
    shutdown_all_mcp_sessions()
    stop_event.set()
    time.sleep(1)
    sys.exit(0)


def initialize_ragflow():
    logging.info(r"""
        ____   ___    ______ ______ __
       / __ \ /   |  / ____// ____// /____  _      __
      / /_/ // /| | / / __ / /_   / // __ \| | /| / /
     / _, _// ___ |/ /_/ // __/  / // /_/ /| |/ |/ /
    /_/ |_|/_/  |_|\____//_/    /_/ \____/ |__/|__/

    """)
    logging.info(f"project base: {get_project_base_directory()}")
    show_configs()
    settings.init_settings()
    print_rag_settings()

    if RAGFLOW_DEBUGPY_LISTEN > 0:
        logging.info(f"debugpy listen on {RAGFLOW_DEBUGPY_LISTEN}")
        import debugpy

        debugpy.listen(("0.0.0.0", RAGFLOW_DEBUGPY_LISTEN))

    # init db
    init_web_db()
    init_web_data()

    RuntimeConfig.init_env()
    RuntimeConfig.init_config(JOB_SERVER_HOST=settings.HOST_IP, HTTP_PORT=settings.HOST_PORT)

    GlobalPluginManager.load_plugins()

    signal.signal(signal.SIGINT, signal_handler)
    signal.signal(signal.SIGTERM, signal_handler)

    def delayed_start_update_progress():
        logging.info("Starting update_progress thread (delayed)")
        t = threading.Thread(target=update_progress, daemon=True)
        t.start()

    def delayed_start_downgrade_guard():
        if os.environ.get("DOWNGRADE_GUARD_ENABLED", "true").lower() == "false":
            logging.info("Downgrade guard disabled via DOWNGRADE_GUARD_ENABLED=false")
            return
        from api.services.downgrade_guard import DowngradeGuard, send_startup_test_email

        guard = DowngradeGuard()
        threading.Thread(target=guard.run_daily_scan, daemon=True, name="downgrade-daily").start()
        threading.Thread(target=guard.run_high_freq_check, daemon=True, name="downgrade-hf").start()
        threading.Thread(target=guard.run_cleanup, daemon=True, name="downgrade-cleanup").start()
        logging.info("Downgrade guard threads started")
        send_startup_test_email()

    if RuntimeConfig.DEBUG:
        if os.environ.get("WERKZEUG_RUN_MAIN") == "true":
            threading.Timer(1.0, delayed_start_update_progress).start()
            threading.Timer(1.0, delayed_start_downgrade_guard).start()
    else:
        threading.Timer(1.0, delayed_start_update_progress).start()
        threading.Timer(1.0, delayed_start_downgrade_guard).start()

    logging.info("RAGFlow WSGI application initialized successfully in production mode")


# Initialize the application when module is imported
initialize_ragflow()

# Export the Flask app for WSGI
application = app
