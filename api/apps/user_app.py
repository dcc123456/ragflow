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
import string
import os
import re
import secrets
import time
import asyncio
from urllib.parse import urlparse, quote
from datetime import datetime
import base64

from api.apps import current_user, login_required, login_user, logout_user
from api.db.services.tenant_llm_service import user_register
from quart import make_response, redirect, request, session
from ldap3 import Server, Connection, ALL, SUBTREE

from api.apps.auth import get_auth_client
from api.db.db_models import TenantLLM
from api.utils.sync_icbccs_user import icbccs_user_register
from common.time_utils import current_timestamp, datetime_format, get_format_time
from common.misc_utils import download_img, get_uuid
from common.constants import RetCode
from common.connection_utils import construct_response
from api.utils.api_utils import (
    get_data_error_result,
    get_json_result,
    get_request_json,
    server_error_response,
    validate_request,
)
from api.utils.crypt import decrypt2
from api.utils.crypt import decrypt
from api.utils.tenant_utils import ensure_tenant_model_id_for_params
from api.utils.web_utils import (
    OTP_LENGTH,
    OTP_TTL_SECONDS,
    ATTEMPT_LIMIT,
    ATTEMPT_LOCK_SECONDS,
    RESEND_COOLDOWN_SECONDS,
    otp_keys,
    hash_code,
    captcha_key,
)
from common import settings
from common.file_utils import get_project_base_directory
from api.db.services.user_service import UserService, TenantService, UserTenantService
from api.db.services.system_settings_service import SystemSettingsService
from api.db.services.role_service import RoleService
from api.db.joint_services.mail_service import send_email_html
from rag.utils.redis_conn import REDIS_CONN
from common.http_client import async_request


async def ldap_login(channel_name: str, username: str, user_password: str):
    login_password = base64.b64decode(decrypt(user_password))
    if isinstance(login_password, (bytes, bytearray)):
        login_password = login_password.decode("utf-8")

    ldap_conf = SystemSettingsService.get_channel_oauth_config(channel_name)
    if not ldap_conf:
        return get_json_result(
            data=False,
            code=RetCode.NOT_FOUND,
            message="LDAP configuration not found.",
        )

    url = ldap_conf.get("url")
    dn =  ldap_conf.get("dn")
    password = ldap_conf.get("password")
    timeout = int(ldap_conf.get("timeout", 10))
    search_base = ldap_conf.get("search_base")
    if not url or not dn:
        return get_json_result(
            data=False,
            code=RetCode.SERVER_ERROR,
            message="LDAP configuration is incomplete.",
        )

    search_filter = ldap_conf.get("search_filter")
    if not search_filter:
        return get_json_result(
            data=False,
            code=RetCode.SERVER_ERROR,
            message="LDAP search_filter is not configured.",
        )
    variables = {"username": username, "login_password": login_password}

    try:
        search_filter = search_filter.format_map(variables)
    except Exception as e:
        return get_json_result(
            data=False,
            code=RetCode.SERVER_ERROR,
            message=f"LDAP search_filter missing variable: {e}",
        )

    attribute_list = ldap_conf.get("attribute_list")
    if isinstance(attribute_list, str):
        attribute_list = [item.strip() for item in attribute_list.split(",") if item.strip()]
    if not attribute_list:
        return get_json_result(
            data=False,
            code=RetCode.SERVER_ERROR,
            message="LDAP attribute_list is not configured.",
        )

    if not search_base:
        dn_parts = dn.split(",", 1)
        search_base = dn_parts[1] if len(dn_parts) > 1 else dn

    logging.info(f"LDAP login attempt: url={url}, dn={dn}, search_base={search_base}, search_filter={search_filter}, attributes={attribute_list}, username={username}")

    parsed = urlparse(url)
    host = parsed.hostname
    port = parsed.port
    use_ssl = False

    def _ldap_search():
        server = Server(host, port=port, get_info=ALL, use_ssl=use_ssl, connect_timeout=timeout)
        admin_conn = Connection(server, user=dn, password=password, auto_bind=True, receive_timeout=timeout)
        admin_conn.search(search_base, search_filter, search_scope=SUBTREE, attributes=attribute_list)
        entries = list(admin_conn.entries)
        admin_conn.unbind()
        return entries

    try:
        result = await asyncio.to_thread(_ldap_search)
    except Exception as e:
        logging.exception("LDAP search failed: ", e)
        return get_json_result(
            data=False,
            code=RetCode.SERVER_ERROR,
            message="LDAP connection failed.",
        )
    if not result:
        return get_json_result(
            data=False,
            code=RetCode.AUTHENTICATION_ERROR,
            message=f"Email: {username} is not registered!",
        )

    try:
        def _ldap_bind_user():
            server = Server(host, port=port, get_info=ALL, use_ssl=use_ssl, connect_timeout=timeout)
            user_conn = Connection(server, user=result[0].entry_dn, password=login_password, auto_bind=False, receive_timeout=timeout)
            ok = user_conn.bind()
            user_conn.unbind()
            return ok

        ok = await asyncio.to_thread(_ldap_bind_user)
        if not ok:
            raise RuntimeError("LDAP user bind failed.")
    except Exception:
        return get_json_result(data=False, code=RetCode.AUTHENTICATION_ERROR, message="Password error!")

    entry = result[0]
    attrs = entry.entry_attributes_as_dict

    def _decode(value, default=""):
        if value is None:
            return default
        if isinstance(value, (bytes, bytearray)):
            return value.decode("utf-8", "ignore")
        return str(value)

    nickname = _decode((attrs.get("givenName") or attrs.get("cn") or [b"Anonymous"])[0], "Anonymous")
    email = _decode((attrs.get("mail") or [username])[0], username)

    avatar = _decode((attrs.get("jpegPhoto") or [b""])[0], "")

    users = UserService.query(email=email)
    if users:
        user = users[0]
        user.access_token = get_uuid()
        login_user(user)
        user.update_time = (current_timestamp(),)
        user.update_date = (datetime_format(datetime.now()),)
        user.save()
        return redirect("/?auth=%s" % user.get_id())

    users = user_register(
        get_uuid(),
        {
            "access_token": get_uuid(),
            "email": email,
            "avatar": avatar,
            "nickname": nickname,
            "login_channel": channel_name,
            "last_login_time": get_format_time(),
            "is_superuser": False,
        },
    )
    if not users:
        raise Exception(f"Fail to register {email}.")
    if len(users) > 1:
        raise Exception(f"Same email: {email} exists!")

    user = users[0]
    login_user(user)
    return redirect("/?auth=%s" % user.get_id())

@manager.route("/login", methods=["POST"])  # noqa: F821
async def login():
    """
    User login endpoint.
    ---
    tags:
      - User
    parameters:
      - in: body
        name: body
        description: Login credentials.
        required: true
        schema:
          type: object
          properties:
            email:
              type: string
              description: User email.
            password:
              type: string
              description: User password.
    responses:
      200:
        description: Login successful.
        schema:
          type: object
      401:
        description: Authentication failed.
        schema:
          type: object
    """
    json_body = await get_request_json()
    if not json_body:
        return get_json_result(data=False, code=RetCode.AUTHENTICATION_ERROR, message="Unauthorized!")

    email = json_body.get("email", "").lower()
    users = UserService.query(email=email)
    if not users:
        return get_json_result(
            data=False,
            code=RetCode.AUTHENTICATION_ERROR,
            message=f"Email: {email} is not registered!",
        )

    password = json_body.get("password")
    try:
        password = decrypt(password)
    except BaseException:
        return get_json_result(data=False, code=RetCode.SERVER_ERROR, message="Fail to crypt password")

    user = UserService.query_user(email, password)

    if user and hasattr(user, 'is_active') and user.is_active == "0":
        return get_json_result(
            data=False,
            code=RetCode.FORBIDDEN,
            message="This account has been disabled, please contact the administrator!",
        )
    elif user:
        response_data = user.to_json()
        user.access_token = get_uuid()
        login_user(user)
        user.update_time = current_timestamp()
        user.update_date = datetime_format(datetime.now())
        user.save()
        msg = "Welcome back!"

        return await construct_response(data=response_data, auth=user.get_id(), message=msg)
    else:
        return get_json_result(
            data=False,
            code=RetCode.AUTHENTICATION_ERROR,
            message="Email and password do not match!",
        )


@manager.route("/login/channels", methods=["GET"])  # noqa: F821
async def get_login_channels():
    """
    Get all supported authentication channels.
    """
    try:
        channels = []
        oauth_config = SystemSettingsService.get_oauth_config()
        for channel, config in oauth_config.items():
            channels.append(
                {
                    "channel": channel,
                    "display_name": config.get("display_name", config.get("name", channel.title())),
                    "icon": config.get("icon", "sso"),
                }
            )
        return get_json_result(data=channels)
    except Exception as e:
        logging.exception(e)
        return get_json_result(data=[], message=f"Load channels failure, error: {str(e)}", code=RetCode.EXCEPTION_ERROR)


@manager.route("/login/<channel>", methods=["GET"])  # noqa: F821
async def oauth_login(channel):
    channel_config = SystemSettingsService.get_channel_oauth_config(channel)
    if not channel_config:
        raise ValueError(f"Invalid channel name: {channel}")
    if channel_config.get("type") == "ldap":
        user_name = request.args.get("username")
        password = request.args.get("password")
        return await ldap_login(channel, user_name, password)

    auth_cli = get_auth_client(channel_config)

    state = get_uuid()
    session["oauth_state"] = state
    auth_url = auth_cli.get_authorization_url(state)
    return redirect(auth_url)


@manager.route("/oauth/callback/<channel>", methods=["GET"])  # noqa: F821
async def oauth_callback(channel):
    """
    Handle the OAuth/OIDC callback for various channels dynamically.
    """
    try:
        channel_config = SystemSettingsService.get_channel_oauth_config(channel)
        if not channel_config:
            raise ValueError(f"Invalid channel name: {channel}")
        auth_cli = get_auth_client(channel_config)

        # Check the state
        state = request.args.get("state")
        if not state or state != session.get("oauth_state"):
            return redirect("/?error=invalid_state")
        session.pop("oauth_state", None)

        # Obtain the authorization code
        code = request.args.get("code")
        if not code:
            return redirect("/?error=missing_code")

        # Exchange authorization code for access token
        if hasattr(auth_cli, "async_exchange_code_for_token"):
            token_info = await auth_cli.async_exchange_code_for_token(code)
        else:
            token_info = auth_cli.exchange_code_for_token(code)
        access_token = token_info.get("access_token")
        if not access_token:
            return redirect("/?error=token_failed")

        id_token = token_info.get("id_token")

        # Fetch user info
        if hasattr(auth_cli, "async_fetch_user_info"):
            user_info = await auth_cli.async_fetch_user_info(access_token, id_token=id_token)
        else:
            user_info = auth_cli.fetch_user_info(access_token, id_token=id_token)
        if not user_info.email:
            return redirect("/?error=email_missing")

        # Login or register
        users = UserService.query(email=user_info.email)
        user_id = get_uuid()

        if not users:
            try:
                try:
                    avatar = await download_img(user_info.avatar_url)
                except Exception as e:
                    logging.exception(e)
                    avatar = ""

                users = user_register(
                    user_id,
                    {
                        "access_token": get_uuid(),
                        "email": user_info.email,
                        "avatar": avatar,
                        "nickname": user_info.nickname,
                        "login_channel": channel,
                        "last_login_time": get_format_time(),
                        "is_superuser": False,
                    },
                )

                if not users:
                    raise Exception(f"Failed to register {user_info.email}")
                if len(users) > 1:
                    raise Exception(f"Same email: {user_info.email} exists!")

                # Try to log in
                user = users[0]
                login_user(user)
                return redirect(f"/?auth={user.get_id()}")

            except Exception as e:
                rollback_user_registration(user_id)
                logging.exception(e)
                encoded_error = quote(str(e))
                return redirect(f"/?error={encoded_error}")

        # User exists, try to log in
        user = users[0]
        user.access_token = get_uuid()
        if user and hasattr(user, 'is_active') and user.is_active == "0":
            return redirect("/?error=user_inactive")

        login_user(user)
        user.save()
        return redirect(f"/?auth={user.get_id()}")
    except Exception as e:
        logging.exception(e)
        encoded_error = quote(str(e))
        return redirect(f"/?error={encoded_error}")


@manager.route("/github_callback", methods=["GET"])  # noqa: F821
async def github_callback():
    """
    **Deprecated**, Use `/oauth/callback/<channel>` instead.

    GitHub OAuth callback endpoint.
    ---
    tags:
      - OAuth
    parameters:
      - in: query
        name: code
        type: string
        required: true
        description: Authorization code from GitHub.
    responses:
      200:
        description: Authentication successful.
        schema:
          type: object
    """
    res = await async_request(
        "POST",
        settings.GITHUB_OAUTH.get("url"),
        data={
            "client_id": settings.GITHUB_OAUTH.get("client_id"),
            "client_secret": settings.GITHUB_OAUTH.get("secret_key"),
            "code": request.args.get("code"),
        },
        headers={"Accept": "application/json"},
    )
    res = res.json()
    if "error" in res:
        return redirect("/?error=%s" % res["error_description"])

    if "user:email" not in res["scope"].split(","):
        return redirect("/?error=user:email not in scope")

    session["access_token"] = res["access_token"]
    session["access_token_from"] = "github"
    user_info = await user_info_from_github(session["access_token"])
    email_address = user_info["email"]
    users = UserService.query(email=email_address)
    user_id = get_uuid()
    if not users:
        # User isn't try to register
        try:
            try:
                avatar = await download_img(user_info["avatar_url"])
            except Exception as e:
                logging.exception(e)
                avatar = ""
            users = user_register(
                user_id,
                {
                    "access_token": session["access_token"],
                    "email": email_address,
                    "avatar": avatar,
                    "nickname": user_info["login"],
                    "login_channel": "github",
                    "last_login_time": get_format_time(),
                    "is_superuser": False,
                },
            )
            if not users:
                raise Exception(f"Fail to register {email_address}.")
            if len(users) > 1:
                raise Exception(f"Same email: {email_address} exists!")

            # Try to log in
            user = users[0]
            login_user(user)
            return redirect("/?auth=%s" % user.get_id())
        except Exception as e:
            rollback_user_registration(user_id)
            logging.exception(e)
            return redirect("/?error=%s" % str(e))

    # User has already registered, try to log in
    user = users[0]
    user.access_token = get_uuid()
    if user and hasattr(user, 'is_active') and user.is_active == "0":
        return redirect("/?error=user_inactive")
    login_user(user)
    user.save()
    return redirect("/?auth=%s" % user.get_id())


@manager.route("/feishu_callback", methods=["GET"])  # noqa: F821
async def feishu_callback():
    """
    Feishu OAuth callback endpoint.
    ---
    tags:
      - OAuth
    parameters:
      - in: query
        name: code
        type: string
        required: true
        description: Authorization code from Feishu.
    responses:
      200:
        description: Authentication successful.
        schema:
          type: object
    """
    feishu_oauth = SystemSettingsService.get_channel_oauth_config("feishu")
    app_access_token_res = await async_request(
        "POST",
        feishu_oauth.get("app_access_token_url"),
        data=json.dumps(
            {
                "app_id": feishu_oauth.get("app_id"),
                "app_secret": feishu_oauth.get("app_secret"),
            }
        ),
        headers={"Content-Type": "application/json; charset=utf-8"},
    )
    app_access_token_res = app_access_token_res.json()
    if app_access_token_res["code"] != 0:
        return redirect("/?error=%s" % app_access_token_res)

    res = await async_request(
        "POST",
        feishu_oauth.get("user_access_token_url"),
        data=json.dumps(
            {
                "grant_type": "authorization_code",
                "code": request.args.get("code"),
            }
        ),
        headers={
            "Content-Type": "application/json; charset=utf-8",
            "Authorization": f"Bearer {app_access_token_res['app_access_token']}",
        },
    )
    res = res.json()
    if res["code"] != 0:
        return redirect("/?error=%s" % res["message"])

    if "contact:user.email:readonly" not in res["data"]["scope"].split():
        return redirect("/?error=contact:user.email:readonly not in scope")
    session["access_token"] = res["data"]["access_token"]
    session["access_token_from"] = "feishu"
    user_info = await user_info_from_feishu(session["access_token"])
    email_address = user_info["email"]
    users = UserService.query(email=email_address)
    user_id = get_uuid()
    if not users:
        # User isn't try to register
        try:
            try:
                avatar = await download_img(user_info["avatar_url"])
            except Exception as e:
                logging.exception(e)
                avatar = ""
            users = user_register(
                user_id,
                {
                    "access_token": session["access_token"],
                    "email": email_address,
                    "avatar": avatar,
                    "nickname": user_info["en_name"],
                    "login_channel": "feishu",
                    "last_login_time": get_format_time(),
                    "is_superuser": False,
                },
            )
            if not users:
                raise Exception(f"Fail to register {email_address}.")
            if len(users) > 1:
                raise Exception(f"Same email: {email_address} exists!")

            # Try to log in
            user = users[0]
            login_user(user)
            return redirect("/?auth=%s" % user.get_id())
        except Exception as e:
            rollback_user_registration(user_id)
            logging.exception(e)
            return redirect("/?error=%s" % str(e))

    # User has already registered, try to log in
    user = users[0]
    if user and hasattr(user, 'is_active') and user.is_active == "0":
        return redirect("/?error=user_inactive")
    user.access_token = get_uuid()
    login_user(user)
    user.save()
    return redirect("/?auth=%s" % user.get_id())


async def user_info_from_feishu(access_token):
    headers = {
        "Content-Type": "application/json; charset=utf-8",
        "Authorization": f"Bearer {access_token}",
    }
    res = await async_request("GET", "https://open.feishu.cn/open-apis/authen/v1/user_info", headers=headers)
    user_info = res.json()["data"]
    user_info["email"] = None if user_info.get("email") == "" else user_info["email"]
    return user_info


async def user_info_from_github(access_token):
    headers = {"Accept": "application/json", "Authorization": f"token {access_token}"}
    res = await async_request("GET", f"https://api.github.com/user?access_token={access_token}", headers=headers)
    user_info = res.json()
    email_info_response = await async_request(
        "GET",
        f"https://api.github.com/user/emails?access_token={access_token}",
        headers=headers,
    )
    email_info = email_info_response.json()
    user_info["email"] = next((email for email in email_info if email["primary"]), None)["email"]
    return user_info


@manager.route("/logout", methods=["GET"])  # noqa: F821
@login_required
async def log_out():
    """
    User logout endpoint.
    ---
    tags:
      - User
    security:
      - ApiKeyAuth: []
    responses:
      200:
        description: Logout successful.
        schema:
          type: object
    """
    current_user.access_token = f"INVALID_{secrets.token_hex(16)}"
    current_user.save()
    logout_user()
    return get_json_result(data=True)


@manager.route("/setting", methods=["POST"])  # noqa: F821
@login_required
async def setting_user():
    """
    Update user settings.
    ---
    tags:
      - User
    security:
      - ApiKeyAuth: []
    parameters:
      - in: body
        name: body
        description: User settings to update.
        required: true
        schema:
          type: object
          properties:
            nickname:
              type: string
              description: New nickname.
            email:
              type: string
              description: New email.
    responses:
      200:
        description: Settings updated successfully.
        schema:
          type: object
    """
    update_dict = {}
    request_data = await get_request_json()
    if request_data.get("password"):
        new_password = request_data.get("new_password")
        #if not check_password_hash(
        #        current_user.password, decrypt(request_data["password"])
        #):
        #    return get_json_result(
        #        data=False,
        #        code=RetCode.AUTHENTICATION_ERROR,
        #        message="Password error!",
        #    )

        if new_password:
            update_dict["password"] = decrypt2(new_password)
            #update_dict["password"] = generate_password_hash(decrypt(new_password))

    for k in request_data.keys():
        if k in [
            "password",
            "new_password",
            "email",
            "status",
            "is_superuser",
            "login_channel",
            "is_anonymous",
            "is_active",
            "is_authenticated",
            "last_login_time",
        ]:
            continue
        update_dict[k] = request_data[k]

    try:
        UserService.update_by_id(current_user.id, update_dict)
        return get_json_result(data=True)
    except Exception as e:
        logging.exception(e)
        return get_json_result(data=False, message="Update failure!", code=RetCode.EXCEPTION_ERROR)


@manager.route("/info", methods=["GET"])  # noqa: F821
@login_required
async def user_profile():
    """
    Get user profile information.
    ---
    tags:
      - User
    security:
      - ApiKeyAuth: []
    responses:
      200:
        description: User profile retrieved successfully.
        schema:
          type: object
          properties:
            id:
              type: string
              description: User ID.
            nickname:
              type: string
              description: User nickname.
            email:
              type: string
              description: User email.
    """
    return get_json_result(data=current_user.to_dict())


def rollback_user_registration(user_id):
    try:
        UserService.delete_by_id(user_id)
    except Exception:
        pass
    try:
        TenantService.delete_by_id(user_id)
    except Exception:
        pass
    try:
        u = UserTenantService.query(tenant_id=user_id)
        if u:
            UserTenantService.delete_by_id(u[0].id)
    except Exception:
        pass
    try:
        TenantLLM.delete().where(TenantLLM.tenant_id == user_id).execute()
    except Exception:
        pass


@manager.route("/register", methods=["POST"])  # noqa: F821
@validate_request("nickname", "email", "password")
async def user_add():
    """
    Register a new user.
    ---
    tags:
      - User
    parameters:
      - in: body
        name: body
        description: Registration details.
        required: true
        schema:
          type: object
          properties:
            nickname:
              type: string
              description: User nickname.
            email:
              type: string
              description: User email.
            password:
              type: string
              description: User password.
    responses:
      200:
        description: Registration successful.
        schema:
          type: object
    """
    req = await get_request_json()
    email_address = req["email"]

    # Validate the email address
    if not re.match(r"^[\w\._-]+@([\w_-]+\.)+[\w-]{2,}$", email_address):
        return get_json_result(
            data=False,
            message=f"Invalid email address: {email_address}!",
            code=RetCode.OPERATING_ERROR,
        )

    # Check if the email address is already used
    if UserService.query(email=email_address):
        return get_json_result(
            data=False,
            message=f"Email: {email_address} has already registered!",
            code=RetCode.OPERATING_ERROR,
        )

    # Construct user info data
    nickname = req["nickname"]
    role_name = settings.DEFAULT_ROLE
    roles = RoleService.get_by_role_name(role_name)
    if not roles:
        get_json_result(
            data=False,
            message=f"Role: {role_name} not exist!",
            code=RetCode.OPERATING_ERROR,
        )
    user_dict = {
        "access_token": get_uuid(),
        "email": email_address,
        "nickname": nickname,
        "password": decrypt(req["password"]),
        "login_channel": "password",
        "last_login_time": get_format_time(),
        "is_superuser": False,
        "role_id": roles[0]["id"]
    }

    user_id = get_uuid()
    try:
        users = user_register(user_id, user_dict)
        if not users:
            raise Exception(f"Fail to register {email_address}.")
        if len(users) > 1:
            raise Exception(f"Same email: {email_address} exists!")
        user = users[0]
        login_user(user)
        return await construct_response(
            data=user.to_json(),
            auth=user.get_id(),
            message=f"{nickname}, welcome aboard!",
        )
    except Exception as e:
        rollback_user_registration(user_id)
        logging.exception(e)
        return get_json_result(
            data=False,
            message=f"User registration failure, error: {str(e)}",
            code=RetCode.EXCEPTION_ERROR,
        )


@manager.route("/tenant_info", methods=["GET"])  # noqa: F821
@login_required
async def tenant_info():
    """
    Get tenant information.
    ---
    tags:
      - Tenant
    security:
      - ApiKeyAuth: []
    responses:
      200:
        description: Tenant information retrieved successfully.
        schema:
          type: object
          properties:
            tenant_id:
              type: string
              description: Tenant ID.
            name:
              type: string
              description: Tenant name.
            llm_id:
              type: string
              description: LLM ID.
            embd_id:
              type: string
              description: Embedding model ID.
    """
    try:
        tenants = TenantService.get_info_by(current_user.id)
        if not tenants:
            return get_data_error_result(message="Tenant not found!")
        return get_json_result(data=tenants[0])
    except Exception as e:
        return server_error_response(e)


@manager.route("/set_tenant_info", methods=["POST"])  # noqa: F821
@login_required
@validate_request("tenant_id", "asr_id", "embd_id", "img2txt_id", "llm_id")
async def set_tenant_info():
    """
    Update tenant information.
    ---
    tags:
      - Tenant
    security:
      - ApiKeyAuth: []
    parameters:
      - in: body
        name: body
        description: Tenant information to update.
        required: true
        schema:
          type: object
          properties:
            tenant_id:
              type: string
              description: Tenant ID.
            llm_id:
              type: string
              description: LLM ID.
            embd_id:
              type: string
              description: Embedding model ID.
            asr_id:
              type: string
              description: ASR model ID.
            img2txt_id:
              type: string
              description: Image to Text model ID.
    responses:
      200:
        description: Tenant information updated successfully.
        schema:
          type: object
    """
    req = await get_request_json()
    try:
        tid = req.pop("tenant_id")
        update_dict = ensure_tenant_model_id_for_params(tid, req)
        TenantService.update_by_id(tid, update_dict)
        return get_json_result(data=True)
    except LookupError as e:
        return get_data_error_result(message=str(e))
    except Exception as e:
        return server_error_response(e)


@manager.route("/forget/captcha", methods=["GET"])  # noqa: F821
async def forget_get_captcha():
    """
    GET /forget/captcha?email=<email>
    - Generate an image captcha and cache it in Redis under key captcha:{email} with TTL = OTP_TTL_SECONDS.
    - Returns the captcha as a PNG image.
    """
    email = (request.args.get("email") or "")
    if not email:
        return get_json_result(data=False, code=RetCode.ARGUMENT_ERROR, message="email is required")

    users = UserService.query(email=email)
    if not users:
        return get_json_result(data=False, code=RetCode.DATA_ERROR, message="invalid email")

    # Generate captcha text
    allowed = string.ascii_uppercase + string.digits
    captcha_text = "".join(secrets.choice(allowed) for _ in range(OTP_LENGTH))
    REDIS_CONN.set(captcha_key(email), captcha_text, 60) # Valid for 60 seconds

    from captcha.image import ImageCaptcha
    image = ImageCaptcha(width=300, height=120, font_sizes=[50, 60, 70])
    img_bytes = image.generate(captcha_text).read()
    response = await make_response(img_bytes)
    response.headers.set("Content-Type", "image/JPEG")
    return response


@manager.route("/forget/otp", methods=["POST"])  # noqa: F821
async def forget_send_otp():
    """
    POST /forget/otp
    - Verify the image captcha stored at captcha:{email} (case-insensitive).
    - On success, generate an email OTP (A–Z with length = OTP_LENGTH), store hash + salt (and timestamp) in Redis with TTL, reset attempts and cooldown, and send the OTP via email.
    """
    req = await get_request_json()
    email = req.get("email") or ""
    captcha = (req.get("captcha") or "").strip()

    if not email or not captcha:
        return get_json_result(data=False, code=RetCode.ARGUMENT_ERROR, message="email and captcha required")

    users = UserService.query(email=email)
    if not users:
        return get_json_result(data=False, code=RetCode.DATA_ERROR, message="invalid email")

    stored_captcha = REDIS_CONN.get(captcha_key(email))
    if not stored_captcha:
        return get_json_result(data=False, code=RetCode.NOT_EFFECTIVE, message="invalid or expired captcha")
    if (stored_captcha or "").strip().lower() != captcha.lower():
        return get_json_result(data=False, code=RetCode.AUTHENTICATION_ERROR, message="invalid or expired captcha")

    # Delete captcha to prevent reuse
    REDIS_CONN.delete(captcha_key(email))

    k_code, k_attempts, k_last, k_lock = otp_keys(email)
    now = int(time.time())
    last_ts = REDIS_CONN.get(k_last)
    if last_ts:
        try:
            elapsed = now - int(last_ts)
        except Exception:
            elapsed = RESEND_COOLDOWN_SECONDS
        remaining = RESEND_COOLDOWN_SECONDS - elapsed
        if remaining > 0:
            return get_json_result(data=False, code=RetCode.NOT_EFFECTIVE, message=f"you still have to wait {remaining} seconds")

    # Generate OTP (uppercase letters only) and store hashed
    otp = "".join(secrets.choice(string.ascii_uppercase) for _ in range(OTP_LENGTH))
    salt = os.urandom(16)
    code_hash = hash_code(otp, salt)
    REDIS_CONN.set(k_code, f"{code_hash}:{salt.hex()}", OTP_TTL_SECONDS)
    REDIS_CONN.set(k_attempts, 0, OTP_TTL_SECONDS)
    REDIS_CONN.set(k_last, now, OTP_TTL_SECONDS)
    REDIS_CONN.delete(k_lock)

    ttl_min = OTP_TTL_SECONDS // 60

    try:
        await send_email_html(
            subject="Your Password Reset Code",
            to_email=email,
            template_key="reset_code",
            code=otp,
            ttl_min=ttl_min,
        )

    except Exception as e:
        logging.exception(e)
        return get_json_result(data=False, code=RetCode.SERVER_ERROR, message="failed to send email")

    return get_json_result(data=True, code=RetCode.SUCCESS, message="verification passed, email sent")


def _verified_key(email: str) -> str:
    return f"otp:verified:{email}"


@manager.route("/forget/verify-otp", methods=["POST"])  # noqa: F821
async def forget_verify_otp():
    """
    Verify email + OTP only. On success:
    - consume the OTP and attempt counters
    - set a short-lived verified flag in Redis for the email
    Request JSON: { email, otp }
    """
    req = await get_request_json()
    email = req.get("email") or ""
    otp = (req.get("otp") or "").strip()

    if not all([email, otp]):
        return get_json_result(data=False, code=RetCode.ARGUMENT_ERROR, message="email and otp are required")

    users = UserService.query(email=email)
    if not users:
        return get_json_result(data=False, code=RetCode.DATA_ERROR, message="invalid email")

    # Verify OTP from Redis
    k_code, k_attempts, k_last, k_lock = otp_keys(email)
    if REDIS_CONN.get(k_lock):
        return get_json_result(data=False, code=RetCode.NOT_EFFECTIVE, message="too many attempts, try later")

    stored = REDIS_CONN.get(k_code)
    if not stored:
        return get_json_result(data=False, code=RetCode.NOT_EFFECTIVE, message="expired otp")

    try:
        stored_hash, salt_hex = str(stored).split(":", 1)
        salt = bytes.fromhex(salt_hex)
    except Exception:
        return get_json_result(data=False, code=RetCode.EXCEPTION_ERROR, message="otp storage corrupted")

    calc = hash_code(otp.upper(), salt)
    if calc != stored_hash:
        # bump attempts
        try:
            attempts = int(REDIS_CONN.get(k_attempts) or 0) + 1
        except Exception:
            attempts = 1
        REDIS_CONN.set(k_attempts, attempts, OTP_TTL_SECONDS)
        if attempts >= ATTEMPT_LIMIT:
            REDIS_CONN.set(k_lock, int(time.time()), ATTEMPT_LOCK_SECONDS)
        return get_json_result(data=False, code=RetCode.AUTHENTICATION_ERROR, message="expired otp")

    # Success: consume OTP and attempts; mark verified
    REDIS_CONN.delete(k_code)
    REDIS_CONN.delete(k_attempts)
    REDIS_CONN.delete(k_last)
    REDIS_CONN.delete(k_lock)

    # set verified flag with limited TTL, reuse OTP_TTL_SECONDS or smaller window
    try:
        REDIS_CONN.set(_verified_key(email), "1", OTP_TTL_SECONDS)
    except Exception:
        return get_json_result(data=False, code=RetCode.SERVER_ERROR, message="failed to set verification state")

    return get_json_result(data=True, code=RetCode.SUCCESS, message="otp verified")


@manager.route("/forget/reset-password", methods=["POST"])  # noqa: F821
async def forget_reset_password():
    """
    Reset password after successful OTP verification.
    Requires: { email, new_password, confirm_new_password }
    Steps:
    - check verified flag in Redis
    - update user password
    - auto login
    - clear verified flag
    """

    req = await get_request_json()
    email = req.get("email") or ""
    new_pwd = req.get("new_password")
    new_pwd2 = req.get("confirm_new_password")

    new_pwd_base64 = decrypt(new_pwd)
    new_pwd_string = base64.b64decode(new_pwd_base64).decode('utf-8')
    new_pwd2_string = base64.b64decode(decrypt(new_pwd2)).decode('utf-8')

    REDIS_CONN.get(_verified_key(email))
    if not REDIS_CONN.get(_verified_key(email)):
        return get_json_result(data=False, code=RetCode.AUTHENTICATION_ERROR, message="email not verified")

    if not all([email, new_pwd, new_pwd2]):
        return get_json_result(data=False, code=RetCode.ARGUMENT_ERROR, message="email and passwords are required")

    if new_pwd_string != new_pwd2_string:
        return get_json_result(data=False, code=RetCode.ARGUMENT_ERROR, message="passwords do not match")

    users = UserService.query_user_by_email(email=email)
    if not users:
        return get_json_result(data=False, code=RetCode.DATA_ERROR, message="invalid email")

    user = users[0]
    try:
        UserService.update_user_password(user.id, new_pwd_base64)
    except Exception as e:
        logging.exception(e)
        return get_json_result(data=False, code=RetCode.EXCEPTION_ERROR, message="failed to reset password")

    # clear verified flag
    try:
        REDIS_CONN.delete(_verified_key(email))
    except Exception:
        pass

    msg = "Password reset successful. Logged in."
    return await construct_response(data=user.to_json(), auth=user.get_id(), message=msg)




@manager.route("/is_admin", methods=["GET"])  # noqa: F821
@login_required
async def is_admin():
    return get_json_result(data={"admin": UserService.is_admin(current_user.id)})


@manager.route("/enable_admin", methods=["GET"])  # noqa: F821
@login_required
async def enable_admin():
    return get_json_result(data={"enable": settings.ENABLE_ADMIN})


@manager.route("/star", methods=["GET"])  # noqa: F821
@login_required
async def has_starred_repo():
    from api.sync_github_star import get_user_stared
    import random
    user = UserService.query(id=current_user.id)
    if not user:
        return get_json_result(
            code=RetCode.UNAUTHORIZED, message="<Unauthorized '401: Unauthorized'>"
        )
    user = user[0].to_dict()
    if user["login_channel"] == "github":
        if REDIS_CONN.get(user["nickname"]):
            return get_json_result(data={"star": True})
        elif random.randint(0, 10) >= 2:
            return get_json_result(data={"star": True})
        else:
            if get_user_stared(user["nickname"]):
                REDIS_CONN.set(user["nickname"], 1, exp=3600*24)
                return get_json_result(data={"star": True})

            return get_json_result(data={"star": False})

    return get_json_result(data={"star": True})


@manager.route("/oauth_callback", methods=["GET"])  # noqa: F821
def casdoor_callback():
    import requests
    base_url = "http://10.142.0.2:8181"
    res = requests.post(
        f"{base_url}/api/login/oauth/access_token",
        data={
            "grant_type": "authorization_code",
            "client_id": "87fe30c13277b95d37b5",
            "client_secret": "2171fdf1fa28f8f29f1eb9aff9af3e0a968ccee6",
            "code": request.args.get("code")
        },
        headers={"Accept": "application/json"},
    )
    res = res.json()
    if "error" in res:
        return redirect("/?error=%s" % res["error_description"])

    access_token = res['access_token']
    res = requests.get(
        f"{base_url}/api/userinfo?accessToken={access_token}",
        headers={"Accept": "application/json"},
    )
    user_info = res.json()
    res = requests.get(
        f"{base_url}/api/get-user?userId={user_info['sub']}",
        headers={"Accept": "application/json", "Authorization": f"Bear {access_token}"},
    )
    user_info = res.json()["data"]
    email_address = user_info["email"]
    users = UserService.query(email=email_address)
    user_id = get_uuid()
    def is_github():
        nonlocal user_info
        return (str(user_info["properties"]) + user_info["avatar"]).lower().find("GitHub") > 0

    if not users:
        # User isn't try to register
        try:
            try:
                avatar = download_img(user_info["avatar"])
            except Exception as e:
                logging.exception(e)
                avatar = user_info["avatar"]
            users = user_register(
                user_id,
                {
                    "access_token": access_token,
                    "email": email_address,
                    "avatar": avatar,
                    "nickname": user_info["displayName"] if user_info.get("displayName") else user_info["name"],
                    "login_channel": "github" if is_github() else "password",
                    "last_login_time": get_format_time(),
                    "update_time": current_timestamp(),
                    "is_superuser": False,
                },
            )
            if len(users) > 1:
                raise Exception(f"Same email: {email_address} exists!")

            # Try to log in
            user = users[0]
            login_user(user)
            return redirect("/?auth=%s" % user.get_id())
        except Exception as e:
            rollback_user_registration(user_id)
            logging.exception(e)
            return redirect("/?error=%s" % str(e))

    # User has already registered, try to log in
    user = users[0]
    user.update_time = current_timestamp()
    user.access_token = get_uuid()
    login_user(user)
    user.save()
    return redirect("/?auth=%s" % user.get_id())
    return redirect("/?auth")


@manager.route("/icbccs_callback", methods=["GET"])  # noqa: F821
def icbccs_callback():
    import requests
    headers = {'Content-Type': 'application/x-www-form-urlencoded'}
    github_oauth = SystemSettingsService.get_channel_oauth_config("github")
    res = requests.post(
        github_oauth.get("url", "https://github.com/login/oauth/access_token"),
        data={
            "client_id": github_oauth.get("client_id"),
            "client_secret": github_oauth.get("secret_key"),
            "code": request.args.get("code"),
            "grant_type": "authorization_code",
            "redirect_uri": github_oauth.get("my_callback_url")
        },
        headers=headers,
    )
    res = res.json()
    if "error" in res:
        return redirect("/?error=%s" % res["error"])

    session["access_token"] = res["access_token"]
    session["access_token_from"] = "icbccs"

    user_info = requests.post(github_oauth.get("usr_url", "https://api.github.com/user"),
                        headers=headers,
                        data={"token": res["access_token"]}
                        ).json()
    email_address = user_info["email"]
    users = UserService.query(email=email_address)
    user_id = get_uuid()
    if not users:
        # User isn't try to register
        try:
            users = icbccs_user_register(user_info["userId"], {
                    "access_token": get_uuid(),
                    "email": user_info["email"],
                    "nickname": user_info["realName"],
                    "login_channel": "icbccs",
                    "last_login_time": get_format_time(),
                    "is_superuser": False,
                    "language": "Chinese"
                })
            if not users:
                raise Exception(f"Fail to register {email_address}.")
            if len(users) > 1:
                raise Exception(f"Same email: {email_address} exists!")

            # Try to log in
            user = users[0]
            login_user(user)
            return redirect("/?auth=%s" % user.get_id())
        except Exception as e:
            rollback_user_registration(user_id)
            logging.exception(e)
            return redirect("/?error=%s" % str(e))

    # User has already registered, try to log in
    user = users[0]
    user.access_token = get_uuid()
    login_user(user)
    user.save()
    return redirect("/?auth=%s" % user.get_id())


def init_saml_auth(req):
    from onelogin.saml2.auth import OneLogin_Saml2_Auth
    import os
    auth = OneLogin_Saml2_Auth(req, custom_base_path=os.path.join(get_project_base_directory(), 'saml'))
    return auth


def prepare_flask_request(request):
    return {
        'https': 'on' if request.scheme == 'https' else 'off',
        #'http_host': request.host,
        'http_host': "kb.innomotics.net",#request.url,
        'server_port': 443,#request.environ.get('SERVER_PORT'),
        'script_name': request.path,
        'get_data': request.args.copy(),
        'post_data': request.form.copy(),
        'query_string': request.query_string.decode('utf-8')
    }


@manager.route('/azure_login') # noqa: F821
def azure_login():
    req = prepare_flask_request(request)
    auth = init_saml_auth(req)
    return redirect(auth.login())


@manager.route('/metadata') # noqa: F821
def metadata():
    req = prepare_flask_request(request)
    auth = init_saml_auth(req)
    settings = auth.get_settings()
    metadata = settings.get_sp_metadata()
    errors = settings.validate_metadata(metadata)

    if not errors:
        return metadata, 200, {'Content-Type': 'text/xml'}
    else:
        return "Metadata error", 500


@manager.route("/azure_callback", methods=["POST"])  # noqa: F821
def azure_callback():
    req = prepare_flask_request(request)
    auth = init_saml_auth(req)
    auth.process_response()
    errors = auth.get_errors()
    print(auth.get_last_error_reason(), auth.is_authenticated(), auth.get_attributes(),  auth.get_nameid(), auth.get_session_index(), "flushssssssssssssssssss", flush=True)

    if errors:
        return auth.get_last_error_reason()

    attr = auth.get_attributes()
    email_address = auth.get_nameid()
    users = UserService.query(email=email_address, status='1')
    user_id = get_uuid()

    if not users:
        #return redirect("/?error=Unauthorized. Contact administrator please.")
        # User isn't try to register
        try:
            users = user_register(
                user_id,
                {
                    "access_token": get_uuid(),
                    "email": email_address,
                    "nickname": attr["http://schemas.microsoft.com/identity/claims/displayname"][0],
                    "login_channel": "entraID",
                    "last_login_time": get_format_time(),
                    "update_time": current_timestamp(),
                    "is_superuser": False,
                },
            )
            if len(users) > 1:
                raise Exception(f"Same email: {email_address} exists!")

            # Try to log in
            user = users[0]
            login_user(user)
            return redirect("/?auth=%s" % user.get_id())
        except Exception as e:
            rollback_user_registration(user_id)
            logging.exception(e)
            return redirect("/?error=%s" % str(e))

    # User has already registered, try to log in
    user = users[0]
    user.update_time = current_timestamp()
    user.access_token = get_uuid()
    user.nickname = attr["http://schemas.microsoft.com/identity/claims/displayname"][0]
    user.login_channel = "entraID"
    login_user(user)
    user.save()
    return redirect("/?auth=%s" % user.get_id())
