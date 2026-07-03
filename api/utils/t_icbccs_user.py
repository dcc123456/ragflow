import logging
import sys
from tqdm import tqdm
from api.db import TeamRole
from api.db.services.team_service import DepartmentMemberService, DepartmentService
from api.db.services.user_service import UserService
from common import settings
from common.misc_utils import get_uuid
from common.time_utils import get_format_time
from api.utils.permission_utils import get_owner_id
from api.utils.sync_icbccs_user import icbccs_user_register

settings.init_settings()


def get_all_user():
    users = [{"userId": f"{i}", "userName": f"t{i}", "email": f"t{i}@t{i}.com", "oaDeptList": [{"deptId": "0", "deptFullName": "dep_01"}]} for i in range(10)]
    for i in range(10, 20):
        users.append({"userId": f"{i}", "userName": f"t{i}", "email": f"t{i}@t{i}.com", "oaDeptList": [{"deptId": "1", "deptFullName": "dep_02"}]})
    return users


def fetch_departments(users, uid):
    depts = {}
    for u in users:
        for d in u.get("oaDeptList", []):
            if d["deptId"] in depts:
                continue
            depts[d["deptId"]] = d["deptFullName"]

    owner_id = get_owner_id(uid, uid)
    for department_id, department_name in depts.items():
        if DepartmentService.filter_by_id(department_id):
            logging.warning(f"Department {department_id} already exists.")
            continue
        department = {
            "id": department_id,
            "name": department_name,
            "path": department_id + "/",
            "formatted_path": department_name + "/",
            "parent_id": department_id,
            "owner_id": owner_id,
            "tenant_id": uid,
        }
        DepartmentService.save(**department)

    return depts


def assign_user2depts(users, uid):
    for u in tqdm(users, "Assign users to departments"):
        for d in u.get("oaDeptList", []):
            department_id = d["deptId"]
            department = DepartmentService.filter_by_id(department_id)
            if not department:
                logging.warning(f"Department {d['deptId']} - {d['deptFullName']} not found.")
                continue
            members = DepartmentMemberService.get_by_department_id(department.id)
            member_ids = [member.member_id for member in members]
            mid = get_owner_id(uid, u["userId"])
            if mid in member_ids:
                logging.warning(f"Member {mid} already exists in department {department.name}.")
                continue

            role = TeamRole.MEMBER if department.owner_id != mid else TeamRole.OWNER
            department_member = {"id": get_uuid(), "department_id": department_id, "member_id": mid, "role": role}
            DepartmentMemberService.save(**department_member)


def sync():
    users = get_all_user()
    for u in tqdm(users, "Sync users"):
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
                    "password": "scrypt:32768:8:1$khHmgSrz6rxySu8M$32a48a3dfa73d356ced5ab54c3381203aecf58eb487bf568ab238909dc54598562b3360463acfbf7c12a92b2e6c04eab5adaba463f41639da156586c31a6c726",
                },
            )
        except Exception as e:
            print(e)

    u = UserService.query(email=sys.argv[1])
    if not u:
        print("Please specify email of the Administrator.")

    fetch_departments(users, u[0].id)
    assign_user2depts(users, u[0].id)


if __name__ == "__main__":
    sync()
