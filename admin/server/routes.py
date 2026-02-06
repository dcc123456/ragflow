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
import json
import asyncio
import secrets
import re
from flask import Blueprint, request
from flask_login import current_user, logout_user, login_required
import pandas as pd

from admin.server.white_list import WhiteListMgr
from auth import login_verify, login_admin, check_admin_auth
from responses import success_response, error_response
from services import UserMgr, ServiceMgr, UserServiceMgr, SettingsMgr, ConfigMgr, EnvironmentsMgr
from roles import RoleMgr, RoleModelMgr
from mail_validator import AsyncSMTPValidator
from admin.server.model_service import ModelMgr
from api.common.exceptions import AdminException
from api.utils.api_utils import get_allowed_llm_factories
from common.versions import get_ragflow_version

admin_bp = Blueprint('admin', __name__, url_prefix='/api/v1/admin')


@admin_bp.route('/ping', methods=['GET'])
def ping():
    return success_response('PONG')


@admin_bp.route('/login', methods=['POST'])
def login():
    if not request.json:
        return error_response('Authorize admin failed.' ,400)
    try:
        email = request.json.get("email", "")
        password = request.json.get("password", "")
        return login_admin(email, password)
    except Exception as e:
        return error_response(str(e), 500)


@admin_bp.route('/logout', methods=['GET'])
@login_required
def logout():
    try:
        current_user.access_token = f"INVALID_{secrets.token_hex(16)}"
        current_user.save()
        logout_user()
        return success_response(True)
    except Exception as e:
        return error_response(str(e), 500)


@admin_bp.route('/auth', methods=['GET'])
@login_verify
def auth_admin():
    try:
        return success_response(None, "Admin is authorized", 0)
    except Exception as e:
        return error_response(str(e), 500)


@admin_bp.route('/users', methods=['GET'])
@login_required
@check_admin_auth
def list_users():
    try:
        users = UserMgr.get_all_users()
        return success_response(users, "Get all users", 0)
    except Exception as e:
        return error_response(str(e), 500)


@admin_bp.route('/users', methods=['POST'])
@login_required
@check_admin_auth
def create_user():
    try:
        data = request.get_json()
        if not data or 'username' not in data or 'password' not in data:
            return error_response("Username and password are required", 400)

        username = data['username']
        password = data['password']
        resource_role = data.get('resource_role', 'owner')
        system_role = data.get('system_role', 'user')

        res = UserMgr.create_user(username, password, resource_role, system_role)
        if res["success"]:
            user_info = res["user_info"]
            user_info.pop("password")  # do not return password
            return success_response(user_info, "User created successfully")
        else:
            return error_response("create user failed")

    except AdminException as e:
        return error_response(e.message, e.code)
    except Exception as e:
        return error_response(str(e))


@admin_bp.route('/users/<username>', methods=['DELETE'])
@login_required
@check_admin_auth
def delete_user(username):
    try:
        res = UserMgr.delete_user(username)
        if res["success"]:
            return success_response(None, res["message"])
        else:
            return error_response(res["message"])

    except AdminException as e:
        return error_response(e.message, e.code)
    except Exception as e:
        return error_response(str(e), 500)


@admin_bp.route('/users/<username>/password', methods=['PUT'])
@login_required
@check_admin_auth
def change_password(username):
    try:
        data = request.get_json()
        if not data or 'new_password' not in data:
            return error_response("New password is required", 400)

        new_password = data['new_password']
        msg = UserMgr.update_user_password(username, new_password)
        return success_response(None, msg)

    except AdminException as e:
        return error_response(e.message, e.code)
    except Exception as e:
        return error_response(str(e), 500)


@admin_bp.route('/users/<username>/activate', methods=['PUT'])
@login_required
@check_admin_auth
def alter_user_activate_status(username):
    try:
        data = request.get_json()
        if current_user.email == username:
            return error_response(f"can't alter current user status: {username}", 409)
        if not data or 'activate_status' not in data:
            return error_response("Activation status is required", 400)
        activate_status = data['activate_status']
        msg = UserMgr.update_user_activate_status(username, activate_status)
        return success_response(None, msg)
    except AdminException as e:
        return error_response(e.message, e.code)
    except Exception as e:
        return error_response(str(e), 500)


@admin_bp.route('/users/<username>/admin', methods=['PUT'])
@login_required
@check_admin_auth
def grant_admin(username):
    try:
        if current_user.email == username:
            return error_response(f"can't grant current user: {username}", 409)
        msg = UserMgr.grant_admin(username)
        return success_response(None, msg)

    except AdminException as e:
        return error_response(e.message, e.code)
    except Exception as e:
        return error_response(str(e), 500)

@admin_bp.route('/users/<username>/admin', methods=['DELETE'])
@login_required
@check_admin_auth
def revoke_admin(username):
    try:
        if current_user.email == username:
            return error_response(f"can't grant current user: {username}", 409)
        msg = UserMgr.revoke_admin(username)
        return success_response(None, msg)

    except AdminException as e:
        return error_response(e.message, e.code)
    except Exception as e:
        return error_response(str(e), 500)

@admin_bp.route('/users/<username>', methods=['GET'])
@login_required
@check_admin_auth
def get_user_details(username):
    try:
        user_details = UserMgr.get_user_details(username)
        return success_response(user_details)

    except AdminException as e:
        return error_response(e.message, e.code)
    except Exception as e:
        return error_response(str(e), 500)


@admin_bp.route('/users/<username>/datasets', methods=['GET'])
@login_required
@check_admin_auth
def get_user_datasets(username):
    try:
        datasets_list = UserServiceMgr.get_user_datasets(username)
        return success_response(datasets_list)

    except AdminException as e:
        return error_response(e.message, e.code)
    except Exception as e:
        return error_response(str(e), 500)


@admin_bp.route('/users/<username>/agents', methods=['GET'])
@login_required
@check_admin_auth
def get_user_agents(username):
    try:
        agents_list = UserServiceMgr.get_user_agents(username)
        return success_response(agents_list)

    except AdminException as e:
        return error_response(e.message, e.code)
    except Exception as e:
        return error_response(str(e), 500)


@admin_bp.route('/services', methods=['GET'])
@login_required
@check_admin_auth
def get_services():
    try:
        services = ServiceMgr.get_all_services()
        return success_response(services, "Get all services", 0)
    except Exception as e:
        return error_response(str(e), 500)


@admin_bp.route('/service_types/<service_type>', methods=['GET'])
@login_required
@check_admin_auth
def get_services_by_type(service_type_str):
    try:
        services = ServiceMgr.get_services_by_type(service_type_str)
        return success_response(services)
    except Exception as e:
        return error_response(str(e), 500)


@admin_bp.route('/services/<service_id>', methods=['GET'])
@login_required
@check_admin_auth
def get_service(service_id):
    try:
        services = ServiceMgr.get_service_details(service_id)
        return success_response(services)
    except Exception as e:
        return error_response(str(e), 500)


@admin_bp.route('/services/<service_id>', methods=['DELETE'])
@login_required
@check_admin_auth
def shutdown_service(service_id):
    try:
        services = ServiceMgr.shutdown_service(service_id)
        return success_response(services)
    except Exception as e:
        return error_response(str(e), 500)


@admin_bp.route('/services/<service_id>', methods=['PUT'])
@login_required
@check_admin_auth
def restart_service(service_id):
    try:
        services = ServiceMgr.restart_service(service_id)
        return success_response(services)
    except Exception as e:
        return error_response(str(e), 500)


@admin_bp.route('/roles', methods=['POST'])
@login_required
@check_admin_auth
def create_role():
    try:
        data = request.get_json()
        if not data or 'role_name' not in data:
            return error_response("Role name is required", 400)
        role_name: str = data['role_name']
        description: str = data['description']
        res = RoleMgr.create_role(role_name, description)
        return success_response(res)
    except Exception as e:
        return error_response(str(e), 500)


@admin_bp.route('/roles/<role_name>', methods=['PUT'])
@login_required
@check_admin_auth
def update_role(role_name: str):
    try:
        data = request.get_json()
        if not data or 'description' not in data:
            return error_response("Role description is required", 400)
        description: str = data['description']
        res = RoleMgr.update_role_description(role_name, description)
        return success_response(res)
    except Exception as e:
        return error_response(str(e), 500)


@admin_bp.route('/roles/<role_name>', methods=['DELETE'])
@login_required
@check_admin_auth
def delete_role(role_name: str):
    try:
        RoleModelMgr.delete_role_default_model(role_name)
        res = RoleMgr.delete_role(role_name)
        return success_response(res)
    except Exception as e:
        return error_response(str(e), 500)


@admin_bp.route('/roles', methods=['GET'])
@login_required
@check_admin_auth
def list_roles():
    try:
        res = RoleMgr.list_roles()
        return success_response(res)
    except Exception as e:
        return error_response(str(e), 500)


@admin_bp.route('/roles_with_permission', methods=['GET'])
@login_required
@check_admin_auth
def list_roles_with_permission():
    try:
        res = RoleMgr.list_roles_with_permission()
        return success_response(res)
    except Exception as e:
        return error_response(str(e), 500)


@admin_bp.route('/roles/<role_name>/permission', methods=['GET'])
@login_required
@check_admin_auth
def get_role_permission(role_name: str):
    try:
        res = RoleMgr.get_role_permission(role_name)
        return success_response(res)
    except Exception as e:
        return error_response(str(e), 500)


@admin_bp.route('/roles/<role_name>/permission', methods=['POST'])
@login_required
@check_admin_auth
def grant_role_permission(role_name: str):
    try:
        data = request.get_json()
        if not data or 'new_permissions' not in data:
            return error_response("Permission is required", 400)
        res = RoleMgr.grant_role_permission(role_name, data['new_permissions'])
        return success_response(res)
    except Exception as e:
        return error_response(str(e), 500)


@admin_bp.route('/roles/<role_name>/permission', methods=['DELETE'])
@login_required
@check_admin_auth
def revoke_role_permission(role_name: str):
    try:
        data = request.get_json()
        if not data or 'revoke_permissions' not in data:
            return error_response("Permission is required", 400)
        res = RoleMgr.revoke_role_permission(role_name, data['revoke_permissions'])
        return success_response(res)
    except Exception as e:
        return error_response(str(e), 500)


@admin_bp.route('/roles/resource', methods=['GET'])
@login_required
@check_admin_auth
def list_roles_resource():
    data = RoleMgr.list_resources()
    return success_response({"resource_types": data})


@admin_bp.route('/roles/<role_name>/default_models', methods=['GET'])
@login_required
@check_admin_auth
def get_role_default_models(role_name: str):
    try:
        data = RoleModelMgr.get_role_default_models(role_name)
        return success_response(data)
    except Exception as e:
        return error_response(str(e), 500)


@admin_bp.route('/roles/<role_name>/default_models', methods=['PUT'])
@login_required
@check_admin_auth
def set_role_default_models(role_name: str):
    try:
        data = request.get_json()
        if not data or "model_type" not in data or "model_id" not in data:
            return error_response("Model type and id are required", 400)
        model_type: str = data['model_type']
        model_id: str = data['model_id']
        res = RoleModelMgr.set_role_default_model(role_name, model_type, model_id, current_user.id)
        return success_response(res)
    except Exception as e:
        return error_response(str(e), 500)


@admin_bp.route('/users/<user_name>/role', methods=['PUT'])
@login_required
@check_admin_auth
def update_user_role(user_name: str):
    try:
        data = request.get_json()
        if not data or 'role_name' not in data:
            return error_response("Role name is required", 400)
        role_name: str = data['role_name']
        res = RoleMgr.update_user_role(user_name, role_name)
        return success_response(res)
    except Exception as e:
        return error_response(str(e), 500)


@admin_bp.route('/users/<user_name>/permission', methods=['GET'])
@login_required
@check_admin_auth
def get_user_permission(user_name: str):
    try:
        res = RoleMgr.get_user_permission(user_name)
        return success_response(res)
    except Exception as e:
        return error_response(str(e), 500)

@admin_bp.route('/variables', methods=['PUT'])
@login_required
@check_admin_auth
def set_variable():
    try:
        data = request.get_json()
        if not data and 'var_name' not in data:
            return error_response("Var name is required", 400)

        if 'var_value' not in data:
            return error_response("Var value is required", 400)
        var_name: str = data['var_name']
        var_value: str = data['var_value']
        if re.match(r'^ldap\|(.+?)\.(enabled|name|url|dn|password|search_filter|attribute_list)$', var_name):
            # allow adding new ldap
            SettingsMgr.update_by_name(var_name, var_value, allow_upsert=True)
        else:
            SettingsMgr.update_by_name(var_name, var_value)
        return success_response(None, "Set variable successfully")
    except AdminException as e:
        return error_response(str(e), 400)
    except Exception as e:
        return error_response(str(e), 500)

@admin_bp.route('/variables', methods=['GET'])
@login_required
@check_admin_auth
def get_variable():
    try:
        if request.content_length is None or request.content_length == 0:
            # list variables
            res = list(SettingsMgr.get_all())
            return success_response(res)

        # get var
        data = request.get_json()
        if not data and 'var_name' not in data:
            return error_response("Var name is required", 400)
        var_name: str = data['var_name']
        res = SettingsMgr.get_by_name(var_name)
        return success_response(res)
    except AdminException as e:
        return error_response(str(e), 400)
    except Exception as e:
        return error_response(str(e), 500)

@admin_bp.route('/variables', methods=['DELETE'])
@login_required
@check_admin_auth
def delete_variable():
    try:
        data = request.get_json()
        if not data or ('var_name' not in data and 'source' not in data):
            return error_response("Var name or source is required", 400)
        var_name: str = data.get('var_name')
        source: str = data.get('source')
        if var_name and source:
            res = SettingsMgr.delete_setting_by_source_and_name(source, var_name)
            return success_response(res)
        elif source:
            res = SettingsMgr.delete_settings_by_source(source)
            return success_response(res)
        else:
            res = SettingsMgr.delete_setting_by_name(var_name)
            return success_response(res)

    except AdminException as e:
        return error_response(str(e), 400)
    except Exception as e:
        return error_response(str(e), 500)


@admin_bp.route('/validate_mail', methods=['POST'])
@login_required
@check_admin_auth
def validate_mail():
    data = request.get_json()
    res = asyncio.run(AsyncSMTPValidator.validate_async(data["host"], data["port"], data["username"], data["password"], data.get("use_tls", False), data.get("use_ssl", False), data.get("timeout", 30)))
    if res["success"]:
        return success_response(data=True)
    else:
        return error_response(data=res["message"])

@admin_bp.route('/configs', methods=['GET'])
@login_required
@check_admin_auth
def get_config():
    try:
        res = list(ConfigMgr.get_all())
        return success_response(res)
    except AdminException as e:
        return error_response(str(e), 400)
    except Exception as e:
        return error_response(str(e), 500)

@admin_bp.route('/environments', methods=['GET'])
@login_required
@check_admin_auth
def get_environments():
    try:
        res = list(EnvironmentsMgr.get_all())
        return success_response(res)
    except AdminException as e:
        return error_response(str(e), 400)
    except Exception as e:
        return error_response(str(e), 500)

@admin_bp.route('/version', methods=['GET'])
@login_required
@check_admin_auth
def show_version():
    try:
        res = {"version": get_ragflow_version()}
        return success_response(res)
    except Exception as e:
        return error_response(str(e), 500)


@admin_bp.route('/whitelist', methods=['GET'])
@login_required
@check_admin_auth
def list_whitelist():
    try:
        res = WhiteListMgr.get_all_white_list()
        return success_response(res)
    except Exception as e:
        return error_response(str(e), 500)


@admin_bp.route('/whitelist/add', methods=['POST'])
@login_required
@check_admin_auth
def create_white_list_row():
    try:
        data = request.get_json()
        if not data or 'email' not in data:
            return error_response("Email is required", 400)
        email: str = data['email']
        res = WhiteListMgr.create_white_list_row(email)
        return success_response(res)
    except Exception as e:
        return error_response(str(e), 500)


@admin_bp.route('/whitelist/<id>', methods=['PUT'])
@login_required
@check_admin_auth
def update_whitelist_row(id: int):
    try:
        data = request.get_json()
        if not data or 'email' not in data:
            return error_response("New email is required", 400)
        email: str = data['email']
        res = WhiteListMgr.update_white_list_row(id, email)
        return success_response(res)
    except Exception as e:
        return error_response(str(e), 500)


@admin_bp.route('/whitelist/<email>', methods=['DELETE'])
@login_required
@check_admin_auth
def delete_whitelist_row(email: str):
    try:
        res = WhiteListMgr.delete_email_from_white_list(email)
        return success_response(res)
    except Exception as e:
        return error_response(str(e), 500)


@admin_bp.route('/whitelist/batch', methods=['POST'])
@login_required
@check_admin_auth
def batch_create_whitelist_rows():
    if 'file' not in request.files:
        return error_response("No file provided", 400)
    file_obj = request.files.get('file')

    blob = file_obj.read()
    df = pd.read_excel(blob)

    data_list = df.to_dict('records')
    emails = [data['email'] for data in data_list]
    try:
        res = WhiteListMgr.batch_create_white_list_rows(emails)
        return success_response(res)
    except Exception as e:
        return error_response(str(e), 500)


@admin_bp.route('/llm/factories', methods=['GET'])
@login_required
@check_admin_auth
def get_factories():
    try:
        factory_list = ModelMgr.get_factories()
        return success_response(factory_list)
    except Exception as e:
        return error_response(str(e), 500)


@admin_bp.route('/llm/set_api_key', methods=['POST'])
@login_required
@check_admin_auth
def set_api_key():
    data = request.get_json()
    try:
        llm_factory = data['llm_factory']
        api_key = data['api_key']
        base_url = data.get('base_url')
        model_type = data.get('model_type')
        llm_name = data.get('llm_name')
        res, msg = asyncio.run(ModelMgr.set_api_key(current_user.id, llm_factory, api_key, base_url, model_type, llm_name))
        if res:
            return success_response(msg)
        return error_response(msg, 500)
    except Exception as e:
        return error_response(str(e), 500)


@admin_bp.route('/llm/add_llm', methods=['POST'])
@login_required
@check_admin_auth
def add_llm():
    data = request.get_json()
    def apikey_json(keys):
        nonlocal data
        return json.dumps({k: data.get(k, "") for k in keys})

    try:
        factory = data["llm_factory"]
        api_key = data.get("api_key", "x")
        llm_name = data.get("llm_name")
        api_base = data.get("api_base")
        model_type = data.get("model_type")
        max_tokens = data.get("max_tokens")

        if factory not in [f.name for f in get_allowed_llm_factories()]:
            return error_response(f"LLM factory {factory} is not allowed", 500)

        if factory == "VolcEngine":
            # For VolcEngine, due to its special authentication method
            # Assemble ark_api_key endpoint_id into api_key
            api_key = apikey_json(["ark_api_key", "endpoint_id"])

        elif factory == "Tencent Hunyuan":
            api_key = apikey_json(["hunyuan_sid", "hunyuan_sk"])
            res, msg = asyncio.run(ModelMgr.set_api_key(current_user.id, factory, api_key, data["base_url"], data["model_type"], llm_name))
            if res:
                return success_response(msg)
            return error_response(msg, 500)

        elif factory == "Tencent Cloud":
            api_key = apikey_json(["tencent_cloud_sid", "tencent_cloud_sk"])
            res, msg = asyncio.run(ModelMgr.set_api_key(current_user.id, factory, api_key, data["base_url"], data["model_type"], llm_name))
            if res:
                return success_response(msg)
            return error_response(msg, 500)

        elif factory == "Bedrock":
            # For Bedrock, due to its special authentication method
            # Assemble bedrock_ak, bedrock_sk, bedrock_region
            api_key = apikey_json(["auth_mode", "bedrock_ak", "bedrock_sk", "bedrock_region", "aws_role_arn"])

        elif factory == "LocalAI":
            llm_name += "___LocalAI"

        elif factory == "HuggingFace":
            llm_name += "___HuggingFace"

        elif factory == "OpenAI-API-Compatible":
            llm_name += "___OpenAI-API"

        elif factory == "VLLM":
            llm_name += "___VLLM"

        elif factory == "XunFei Spark":
            if data["model_type"] == "chat":
                api_key = data.get("spark_api_password", "")
            elif data["model_type"] == "tts":
                api_key = apikey_json(["spark_app_id", "spark_api_secret", "spark_api_key"])

        elif factory == "BaiduYiyan":
            api_key = apikey_json(["yiyan_ak", "yiyan_sk"])

        elif factory == "Fish Audio":
            api_key = apikey_json(["fish_audio_ak", "fish_audio_refid"])

        elif factory == "Google Cloud":
            api_key = apikey_json(["google_project_id", "google_region", "google_service_account_key"])

        elif factory == "Azure-OpenAI":
            api_key = apikey_json(["api_key", "api_version"])

        elif factory == "OpenRouter":
            api_key = apikey_json(["api_key", "provider_order"])

        elif factory == "MinerU":
            api_key = apikey_json(["api_key", "provider_order"])

        res, msg = ModelMgr.add_llm(current_user.id, factory, api_key, llm_name, model_type, api_base, max_tokens)
        if res:
            return success_response(msg)
        return error_response(msg, 500)

    except Exception as e:
        return error_response(str(e), 500)


@admin_bp.route('/llm/delete_factory', methods=['POST'])
@login_required
@check_admin_auth
def delete_factory():
    data = request.get_json()
    try:
        ModelMgr.delete_factory(current_user.id, data["llm_factory"])
        return success_response(data=True)
    except Exception as e:
        return error_response(str(e), 500)


@admin_bp.route('/llm/my_llms', methods=['GET'])
@login_required
@check_admin_auth
def get_my_llms():
    include_details = request.args.get("include_details", "false").lower() == "true"
    try:
        res = ModelMgr.get_my_llms(current_user.id, include_details)
        return success_response(res)
    except Exception as e:
        return error_response(str(e), 500)


@admin_bp.route('/llm/list', methods=['GET'])
@login_required
@check_admin_auth
def list_app():
    model_type = request.args.get("model_type")
    try:
        success, res = ModelMgr.list_app(current_user.id, model_type)
        if success:
            return success_response(res)
        return error_response(res, 500)
    except Exception as e:
        return error_response(str(e), 500)
