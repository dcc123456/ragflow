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

import time

start_ts = time.time()

import os
import signal
import logging
import threading
import faulthandler

from flask import Flask, jsonify, Response

from api.utils.health_utils import run_health_checks
from api.db.db_models import close_connection
from flask_login import LoginManager
from werkzeug.serving import run_simple
from routes import admin_bp
from common.log_utils import init_root_logger
from common.constants import SERVICE_CONF
from common.config_utils import show_configs
from common import settings
from config import load_configurations, SERVICE_CONFIGS
from auth import init_default_admin, setup_auth
from flask_session import Session
from common.versions import get_ragflow_version
from prometheus_client import generate_latest, CONTENT_TYPE_LATEST
from admin.server.admin_metrics import admin_metrics_worker

stop_event = threading.Event()

if __name__ == "__main__":
    faulthandler.enable()
    init_root_logger("admin_service")
    logging.info(r"""
        ____  ___   ______________                 ___       __          _
       / __ \/   | / ____/ ____/ /___ _      __   /   | ____/ /___ ___  (_)___
      / /_/ / /| |/ / __/ /_  / / __ \ | /| / /  / /| |/ __  / __ `__ \/ / __ \
     / _, _/ ___ / /_/ / __/ / / /_/ / |/ |/ /  / ___ / /_/ / / / / / / / / / /
    /_/ |_/_/  |_\____/_/   /_/\____/|__/|__/  /_/  |_\__,_/_/ /_/ /_/_/_/ /_/
    """)

    app = Flask(__name__)

    @app.teardown_request
    def _db_close(exception):
        if exception:
            logging.exception(f"Request failed: {exception}")
        close_connection()

    # =============================================================================
    # Health check routes for Kubernetes liveness/readiness probes
    # =============================================================================
    # Liveness probe + GKE Gateway NEG health check
    # - K8s livenessProbe: /live (configured in Terraform)
    # - GKE Gateway NEG: uses "/" by default (cannot be configured)
    # Both return 200 OK without checking dependencies to avoid unnecessary pod restarts
    @app.route("/", methods=["GET"])
    @app.route("/live", methods=["GET"])
    def liveness():
        """
        Lightweight liveness probe for Kubernetes and GKE Gateway NEG health check.
        Returns 200 OK immediately without checking any dependencies.
        - K8s livenessProbe uses this to determine if the container should be restarted
        - GKE Gateway NEG uses "/" by default for health check
        """
        return "", 200

    # Readiness probe: comprehensive health check including all dependencies
    @app.route("/healthz", methods=["GET"])
    def healthz():
        """
        Health check endpoint for Kubernetes probes.
        Returns health status of all dependencies (DB, Redis, storage, etc.)
        """
        from api.db.db_models import DB

        with DB.connection_context():
            result, all_ok = run_health_checks()
        if all_ok:
            logging.info(f"healthz result: {result}, all_ok: {all_ok}")
        else:
            logging.warn(f"healthz result: {result}, all_ok: {all_ok}")
        return jsonify(result), (200 if all_ok else 500)

    @app.get("/metrics")
    def metrics():
        data = generate_latest()
        return Response(response=data, status=200, content_type=CONTENT_TYPE_LATEST)

    app.register_blueprint(admin_bp)
    app.config["SESSION_PERMANENT"] = False
    app.config["SESSION_TYPE"] = "filesystem"
    app.config["MAX_CONTENT_LENGTH"] = int(os.environ.get("MAX_CONTENT_LENGTH", 1024 * 1024 * 1024))
    # Initialize settings to get SECRET_KEY before Session is configured
    settings.init_settings()
    app.secret_key = settings.get_secret_key()
    Session(app)
    logging.info(f"RAGFlow admin version: {get_ragflow_version()}")
    show_configs()
    login_manager = LoginManager()
    login_manager.init_app(app)
    # Ensure SECRET_KEY is properly set for auth module
    from common import settings as _settings
    from admin.server import auth as _auth

    _auth.settings = _settings
    setup_auth(login_manager)
    init_default_admin()
    # init_user_role()
    SERVICE_CONFIGS.configs = load_configurations(SERVICE_CONF)

    admin_metrics_thread = threading.Thread(target=admin_metrics_worker, args=(60,), daemon=True)
    admin_metrics_thread.start()

    try:
        logging.info(f"RAGFlow admin is ready after {time.time() - start_ts}s initialization.")
        run_simple(
            hostname="0.0.0.0",
            port=9381,
            application=app,
            threaded=True,
            use_reloader=False,
            use_debugger=False,
        )
    except Exception as e:
        logging.exception(f"Unhandled exception: {e}")
        stop_event.set()
        time.sleep(1)
        os.kill(os.getpid(), signal.SIGKILL)
