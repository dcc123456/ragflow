import logging
import sys

import requests

from common import settings
from api.db import TeamRole, UserTenantRole, FileType
from api.db.services.file_service import FileService
from api.db.services.team_service import DepartmentMemberService, DepartmentService
from api.db.services.user_service import UserService, TenantService, UserTenantService
from common.time_utils import get_format_time
from common.misc_utils import get_uuid


settings.init_settings()


def icbccs_user_register(user_id, user):
    user["id"] = user_id
    tenant = {
        "id": user_id,
        "name": user["nickname"] + "‘s Kingdom",
        "llm_id": settings.CHAT_MDL,
        "embd_id": settings.EMBEDDING_MDL,
        "asr_id": settings.ASR_MDL,
        "parser_ids": settings.PARSERS,
        "img2txt_id": settings.IMAGE2TEXT_MDL,
        "rerank_id": settings.RERANK_MDL,
    }
    usr_tenant = {
        "tenant_id": user_id,
        "user_id": user_id,
        "invited_by": user_id,
        "role": UserTenantRole.OWNER,
    }
    file_id = get_uuid()
    file = {
        "id": file_id,
        "parent_id": file_id,
        "tenant_id": user_id,
        "created_by": user_id,
        "name": "/",
        "type": FileType.FOLDER.value,
        "size": 0,
        "location": "",
    }
    if not UserService.save(**user):
        raise Exception("Fail to save.")

    TenantService.insert(**tenant)
    UserTenantService.insert(**usr_tenant)
    FileService.insert(file)
    return UserService.query(email=user["email"])


def get_all_user(IP_PORT):
    res = requests.post(f"http://{IP_PORT}/user/employee", headers={"Content-Type": "application/x-www-form-urlencoded"}, data={"clientId": "isearch", "clientSecret": "bfj3sdc4qgdj"})
    return res.json()


def init_departments(users, tenant_id):
    depts = {}
    for u in users:
        user_tenant = {
            "tenant_id": tenant_id,
            "user_id": u["userId"],
            "invited_by": tenant_id,
            "role": UserTenantRole.NORMAL,
        }
        if not UserTenantService.filter_by_tenant_and_user_id(tenant_id, u["userId"]):
            UserTenantService.save(**user_tenant)

        for d in u.get("oaDeptList", []):
            if d["deptId"] in depts:
                continue
            depts[d["depts"]] = d["deptNameFullName"]

    # TODO: @leiyongteng
    owner = UserTenantService.filter_by_tenant_and_user_id(tenant_id, tenant_id)
    if not owner:
        logging.warning(f"!!! The Department Owner(UserTenant) of {tenant_id} not found.")
        return
    owner_id = owner.id
    for department_id, department_name in depts.items():
        department = {
            "id": department_id,
            "name": department_name,
            "path": department_id + "/",
            "formatted_path": department_name + "/",
            "parent_id": department_id,
            "owner_id": owner_id,
            "tenant_id": tenant_id,
        }
        if DepartmentService.filter_by_id(department_id):
            logging.warning(f"Department {department_id} already exists.")
            continue
        DepartmentService.save(**department)


def assign_user2depts(users, tenant_id):
    # TODO: @leiyongteng
    for u in users:
        depts = {}
        for d in u.get("oaDeptList", []):
            if d["deptId"] in depts:
                continue
            depts[d["depts"]] = d["deptNameFullName"]

            # check_department
            department = DepartmentService.filter_by_id(d["deptId"])
            if not department:
                logging.warning(f"Department {d['deptId']} - {d['deptNameFullName']} not found.")
                continue

            members = DepartmentMemberService.get_by_department_id(department.id)
            member_ids = [member.member_id for member in members]

            member = UserTenantService.filter_by_tenant_and_user_id(user_id=u["userId"], tenant_id=tenant_id)
            if member.id in member_ids:
                logging.warning(f"Member {member.id} already exists in department {department.name}.")
                continue

            role = TeamRole.MEMBER if department.owner_id != member.id else TeamRole.OWNER
            department_member = {"id": get_uuid(), "department_id": d["deptId"], "member_id": member.id, "role": role}
            DepartmentMemberService.save(**department_member)


def sync(IP_PORT, tennant_id=None):
    users = get_all_user(IP_PORT)
    for u in users:
        # "oaDeptList": [{"deptId": "9999", "deptNameFullName": ""}]
        e = UserService.query(email=u["email"])
        if e is not None and e:
            print(u["userName"] + " is already there!")
            continue
        try:
            icbccs_user_register(
                u["userId"],
                {
                    "access_token": get_uuid(),
                    "email": u["email"],
                    "nickname": u["userName"],
                    "login_channel": "icbccs",
                    "last_login_time": get_format_time(),
                    "is_superuser": False,
                    "language": "Chinese",
                },
            )
        except Exception as e:
            print(e)

    init_departments(users, tennant_id)
    assign_user2depts(users, tennant_id)


if __name__ == "__main__":
    sync(sys.argv[1])
