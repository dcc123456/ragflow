
from api.db import MANAGEMENT_TEAM_ROLES, VALID_TEAM_ROLES, TeamRole
from api.db.db_models import DB
from api.db.services.permission_service import PermissionService
from api.db.services.team_service import DepartmentMemberService, DepartmentService, GroupMemberService, GroupService
from api.db.services.user_service import UserTenantService
from api.utils.api_utils import get_data_error_result, get_error_data_result, get_json_result, server_error_response, validate_request
from common.time_utils import delta_seconds
from common.misc_utils import get_uuid
from common.constants import StatusEnum, RetCode
from common.role_util import TEAM_API_ACTION_MAP, TEAM_ROLE_RESOURCE_TYPE, check_role_access
from quart import request
from api.apps import login_required, current_user

team_role_guard = check_role_access(TEAM_API_ACTION_MAP, TEAM_ROLE_RESOURCE_TYPE)

# =========================================== GROUP ============================


@manager.route("<tenant_id>/groups", methods=["GET"])  # noqa: F821
@login_required
@team_role_guard
def group_list(tenant_id):
    """
    Retrieve the list of user team groups.

    Returns:
        JSON: List of groups
    """
    if not tenant_id:
        return get_data_error_result(message="Missing required filed `tenant_id`")

    try:
        member = UserTenantService.filter_by_tenant_and_user_id(tenant_id=tenant_id, user_id=current_user.id)
        if not member:
            return get_data_error_result(message="Unrecognized identification.")

        # is_team_owner = tenant_id == current_user.id

        # if is_team_owner:
        #     groups = GroupService.get_groups_by_tenant_id(tenant_id=tenant_id)
        #     for group in groups:
        #         group["members"] = GroupMemberService.get_by_group_id_with_info(group["group_id"])
        # else:
        #     groups = GroupMemberService.get_groups_by_member_id(member.id)

        groups = GroupService.get_groups_by_tenant_id(tenant_id=tenant_id)
        for group in groups:
            group["members"] = GroupMemberService.get_by_group_id_with_info(group["group_id"])

        for group in groups:
            group["delta_seconds"] = delta_seconds(str(group["update_date"]))
        return get_json_result(data=groups)
    except Exception as e:
        return server_error_response(e)


@manager.route("/group/create", methods=["POST"])  # noqa: F821
@login_required
@team_role_guard
@validate_request("name")  # noqa: F821
async def create_group():
    """
    Create a new team group.

    Request Body:
        {
            "name": "A group",
            "avatar": "default.avatar"
        }

    Returns:
        JSON: Success message with group details
    """
    req = await request.get_json()
    tenant_id = current_user.id
    name = req.get("name")
    if not name:
        return get_data_error_result(message="Missing required field `name`.")

    if GroupService.query_group(tenant_id=tenant_id, group_name=name):
        return get_data_error_result(message=f"Group `{name}` is already in the team.")

    owner = UserTenantService.filter_by_tenant_and_user_id(tenant_id, tenant_id)
    if not owner:
        return get_data_error_result(message="Unrecognized identification.")
    owner_id = owner.id

    try:
        with DB.atomic():
            group = dict(
                id=get_uuid(),
                name=name,
                avatar=req.get("avatar"),
                owner_id=owner_id,
                tenant_id=tenant_id,
            )
            GroupService.save(**group)

            member = dict(
                id=get_uuid(),
                member_id=owner_id,
                group_id=group["id"],
                role=TeamRole.OWNER,
            )
            GroupMemberService.save(**member)

            return get_json_result(data=group)
    except Exception as e:
        server_error_response(e)


@manager.route("<tenant_id>/group/delete/<group_id>", methods=["DELETE"])  # noqa: F821
@login_required
@team_role_guard
def delete_group(tenant_id, group_id):
    """
    Delete a specific team group.

    Query Parameters:
        group_id (string): The ID of the group to be deleted

    Returns:
        JSON: Success message confirming deletion
    """
    g = GroupService.filter_by_id(group_id)
    if not g:
        return get_data_error_result(message=f"Group `{group_id}` does not exist.")
    group_owner_id = g.owner_id

    is_team_owner = g.tenant_id == current_user.id

    operator = UserTenantService.filter_by_tenant_and_user_id(tenant_id, current_user.id)
    if not operator:
        return get_data_error_result(message="Unrecognized identification.")

    if not (is_team_owner or group_owner_id == operator.id):
        return get_json_result(data=False, message="Permission denied", code=RetCode.PERMISSION_ERROR)

    member_model_list = GroupMemberService.get_by_group_id(group_id)
    try:
        with DB.atomic():
            GroupMemberService.delete(member_model_list)
            GroupService.delete_group(group_id)

            permissions = PermissionService.get_permissions_by_tenant_and_group_id_with_types(tenant_id=tenant_id, group_id=group_id)
            PermissionService.delete(permissions)

            return get_json_result(data=True)
    except Exception as e:
        server_error_response(e)


@manager.route("<tenant_id>/group/owner", methods=["PUT"])  # noqa: F821
@login_required
@team_role_guard
@validate_request("group_id", "new_owner_id", "remain_admin")  # noqa: F821
async def group_change_owner(tenant_id):
    req = await request.get_json()
    group_id = req.get("group_id")
    if not group_id:
        return get_data_error_result(message="Missing required field `group_id`.")
    new_owner_id = req.get("new_owner_id")
    if not new_owner_id:
        return get_data_error_result(message="Missing required field `new_owner_id`.")
    remain_admin = req.get("remain_admin", False)

    group = GroupService.filter_by_id(group_id)
    if not group:
        return get_data_error_result(message=f"Group `{group_id}` does not exist.")
    group_owner_id = group.owner_id

    operator = UserTenantService.filter_by_tenant_and_user_id(tenant_id=tenant_id, user_id=current_user.id)
    if not (tenant_id == current_user.id or (operator and operator.id == group_owner_id)):
        return get_json_result(data=False, message="Permission denied", code=RetCode.PERMISSION_ERROR)

    new_owner = GroupMemberService.filter_by_group_and_member_id_ignore_validity(group_id=group.id, member_id=new_owner_id)
    if not new_owner:
        with DB.atomic():
            try:
                new_owner_dict = dict(id=get_uuid(), member_id=new_owner_id, group_id=group.id, role=TeamRole.OWNER, status=StatusEnum.INVALID.value)
                new_owner = GroupMemberService.save(**new_owner_dict)
            except Exception:
                return get_error_data_result(message="Internal error")

    if not new_owner:
        return get_error_data_result(message="Internal error")

    to_update = [new_owner]
    new_owner.role = TeamRole.OWNER
    new_owner.status = StatusEnum.VALID.value
    group.owner_id = new_owner_id

    old_owner = GroupMemberService.filter_by_group_and_member_id(group_id=group.id, member_id=group_owner_id)
    if old_owner:
        old_owner.role = TeamRole.ADMIN if remain_admin else TeamRole.MEMBER
        to_update.append(old_owner)

    with DB.atomic():
        try:
            GroupService.update_group_model(group_model=group)
            GroupMemberService.update_many(member_model_list=to_update, allow_to_update=["role", "status"])
        except Exception:
            return get_error_data_result(message="Internal error")

    return get_json_result(data=True)


@manager.route("/<tenant_id>/group/update", methods=["PUT"])  # noqa: F821
@login_required
@team_role_guard
@validate_request("group_id")  # noqa: F821
async def update_group(tenant_id):
    """
    Update team group information, including the member list and roles.
    Allowed to update group fields:  ["name", "avatar", "status"]
    Allowed to update group member fields: ["group_id", "role", "status"]
    Member_list is optional

    Request Body:
        {
            "group_id": "67c47ec6fbbc1fac38479289",
            "member_list": [
                {
                    "member_id": "67bed779548836545cdf37b5",
                    "role": "admin"
                },
                {
                    "member_id": "67bed99cf57aa5b4c309e305",
                    "role": "owner"
                }
            ]
        }

    Returns:
        JSON: Success message confirming update
    """
    req = await request.get_json()
    group_id = req.get("group_id")
    if not group_id:
        return get_data_error_result(message="Missing required field `group_id`.")

    group = GroupService.filter_by_id(group_id)
    if not group:
        return get_data_error_result(message=f"Group `{group_id}` does not exist.")
    group_owner = group.owner_id

    operator = UserTenantService.filter_by_tenant_and_user_id(tenant_id=tenant_id, user_id=current_user.id)
    if not operator:
        return get_data_error_result(message="Unrecognized identification.")

    is_group_owner = operator.id == group_owner
    is_team_owner = tenant_id == current_user.id

    if not (is_team_owner or is_group_owner):
        group_operator = GroupMemberService.filter_by_group_and_member_id(group_id=group_id, member_id=operator.id)
        if not group_operator:
            return get_data_error_result(message="Unrecognized group identification.")

        if group_operator.role not in MANAGEMENT_TEAM_ROLES:
            return get_json_result(data=False, message="Permission denied", code=RetCode.PERMISSION_ERROR)

    allow_to_update = ["name", "avatar", "status"]
    group_dict = {r: req[r] for r in req if r in allow_to_update}

    member_allow_to_update = ["group_id", "role", "status"]
    members_to_update = []
    members_to_insert = []
    members_to_delete = []
    member_list = req.get("member_list", [])
    if member_list:
        for member_info in req["member_list"]:
            is_deletion = False
            if "role" in member_info and "member_id" in member_info:
                member_id = member_info["member_id"]
                role = member_info["role"]
                if (role not in VALID_TEAM_ROLES) or (role == TeamRole.OWNER and member_id != group_owner):
                    continue

                member = GroupMemberService.filter_by_group_and_member_id(group_id, member_id)
                if not member:
                    if not UserTenantService.filter_by_id(member_id):
                        continue
                    member_info["group_id"] = group_id
                    members_to_insert.append(member_info)
                    continue

                for field in member_allow_to_update:
                    if field in member_info:
                        if field == "role" and (member.role == TeamRole.OWNER or member.role == operator.role):
                            continue
                        if field == "status" and member_info[field] == StatusEnum.INVALID.value:
                            is_deletion = True
                        setattr(member, field, member_info[field])

                if is_deletion:
                    members_to_delete.append(member)
                else:
                    members_to_update.append(member)
    try:
        with DB.atomic():
            GroupService.update_group(group_id, group_dict)

            if members_to_insert:
                GroupMemberService.insert_many(members_to_insert)

            if members_to_update:
                GroupMemberService.update_many(members_to_update, member_allow_to_update)

            if members_to_delete:
                GroupMemberService.update_many(members_to_delete, member_allow_to_update)

                for member in members_to_delete:
                    permissions = PermissionService.get_permissions_by_tenant_and_member_id_with_types(tenant_id=tenant_id, member_id=member.member_id)
                    PermissionService.delete(permissions)

        return get_json_result(data=True)

    except Exception as e:
        server_error_response(e)


@manager.route("<tenant_id>/group/members/<group_id>", methods=["GET"])  # noqa: F821
@login_required
@team_role_guard
def list_group_member(tenant_id, group_id):
    """
    List members of specific group
    """
    group = GroupService.filter_by_id(group_id)
    if not group:
        return get_data_error_result(message=f"Group `{group_id}` does not exist.")
    group_owner = group.owner_id

    checker = UserTenantService.filter_by_tenant_and_user_id(tenant_id=tenant_id, user_id=current_user.id)
    if not checker:
        return get_data_error_result(message="Unrecognized identification.")

    is_team_owner = tenant_id == current_user.id

    if not is_team_owner:
        if checker.id != group_owner:
            if not GroupMemberService.filter_by_group_and_member_id(group_id=group_id, member_id=checker.id):
                return get_data_error_result(message="Unrecognized group identification.")

    member_list = GroupMemberService.get_by_group_id_with_info(group_id)

    return get_json_result(data=member_list)


@manager.route("<tenant_id>/group/member/create", methods=["POST"])  # noqa: F821
@login_required
@team_role_guard
@validate_request("group_id", "member_list")  # noqa: F821
async def add_group_member(tenant_id):
    """
    Add multiple members to a group.

    Request Body:
        {
            "group_id": "67c47bd8f2e078c384fe4a72",
            "member_list": [
                {
                    "member_id": "67bed779548836545cdf37b5",
                    "role": "admin"
                },
                {
                    "member_id": "67bed779548836545cdf37b6",
                    "role": "member"
                }
            ]
        }

    Returns:
        JSON: Success message confirming addition
    """
    req = await request.get_json()
    group_id = req.get("group_id")
    member_list = req.get("member_list", [])

    if not group_id or not member_list:
        return get_data_error_result(message="Missing required fields: `group_id`, `member_list`.")

    g = GroupService.filter_by_id(group_id)
    if not g:
        return get_data_error_result(message=f"Group `{group_id}` does not exist.")
    group_owner = g.owner_id

    operator = UserTenantService.filter_by_tenant_and_user_id(tenant_id=tenant_id, user_id=current_user.id)
    if not operator:
        return get_data_error_result(message="Unrecognized identification.")

    is_group_owner = operator.id == group_owner
    is_team_owner = tenant_id == current_user.id

    if not (is_group_owner or is_team_owner):
        group_operator = GroupMemberService.filter_by_group_and_member_id(group_id=group_id, member_id=operator.id)
        if not group_operator:
            return get_data_error_result(message="Unrecognized group identification.")

        if group_operator.role not in MANAGEMENT_TEAM_ROLES:
            return get_json_result(data=False, message="Permission denied", code=RetCode.PERMISSION_ERROR)

    not_added = []
    members = []
    for member_info in member_list:
        if "role" in member_info and "member_id" in member_info:
            member_id = member_info["member_id"]
            role = member_info["role"]

            if (role not in VALID_TEAM_ROLES) or (role == TeamRole.OWNER) or (member_id == group_owner):
                not_added.append(member_id)
                continue

            if not UserTenantService.filter_by_id(member_id):
                not_added.append(member_id)
                continue

            if GroupMemberService.filter_by_group_and_member_id(group_id, member_id):
                not_added.append(member_id)
                continue

            member = {"id": get_uuid(), "group_id": group_id, "member_id": member_id, "role": role}
            members.append(member)
    try:
        with DB.atomic():
            GroupMemberService.insert_many(members)
        return get_json_result(data={"group_id": group_id, "members_added": members, "members_not_added": not_added})

    except Exception as e:
        server_error_response(e)


@manager.route("/<tenant_id>/group/member/delete", methods=["DELETE"])  # noqa: F821
@login_required
@team_role_guard
@validate_request("group_id", "member_list")  # noqa: F821
async def remove_group_member(tenant_id):
    """
    Delete multiple members to a group.

    Request Body:
        {
            "group_id": "67c47bd8f2e078c384fe4a72",
            "member_list": [
                {
                    "member_id": "67bed779548836545cdf37b5",
                    "role": "admin"
                },
                {
                    "member_id": "67bed779548836545cdf37b6",
                    "role": "member"
                }
            ]
        }

    Returns:
        JSON: Success message confirming addition
    """
    req = await request.get_json()
    group_id = req.get("group_id")
    member_list = req.get("member_list", [])

    if not group_id or not member_list:
        return get_data_error_result(message="Missing required fields: `group_id`, `member_list`.")

    g = GroupService.filter_by_id(group_id)
    if not g:
        return get_data_error_result(message=f"Group `{group_id}` does not exist.")
    group_owner = g.owner_id

    operator = UserTenantService.filter_by_tenant_and_user_id(tenant_id=tenant_id, user_id=current_user.id)
    if not operator:
        return get_data_error_result(message="Unrecognized identification.")

    is_group_owner = operator.id == group_owner
    is_team_owner = tenant_id == current_user.id

    if not (is_group_owner or is_team_owner):
        group_operator = GroupMemberService.filter_by_group_and_member_id(group_id=group_id, member_id=operator.id)
        if not group_operator:
            return get_data_error_result(message="Unrecognized group identification.")

        if group_operator.role not in MANAGEMENT_TEAM_ROLES:
            return get_json_result(data=False, message="Permission denied", code=RetCode.PERMISSION_ERROR)

    not_deleted = []
    member_ids = []
    for member_info in member_list:
        if "role" in member_info and "member_id" in member_info:
            member_id = member_info["member_id"]
            role = member_info["role"]

            if (role not in VALID_TEAM_ROLES) or (role == TeamRole.OWNER):
                not_deleted.append(member_id)
                continue

            member_ids.append(member_id)

    members = GroupMemberService.filter_by_group_and_member_ids(group_id, member_ids)
    try:
        with DB.atomic():
            if members:
                GroupMemberService.delete(members)

                for member in members:
                    permissions = PermissionService.get_permissions_by_tenant_and_member_id_with_types(tenant_id=tenant_id, member_id=member.member_id)
                    PermissionService.delete(permissions)

        return get_json_result(data={"group_id": group_id, "members_deleted": member_ids, "members_not_deleted": not_deleted})

    except Exception as e:
        server_error_response(e)


# =========================================== DEPARTMENT ============================


@manager.route("<tenant_id>/departments", methods=["GET"])  # noqa: F821
@login_required
@team_role_guard
def department_list(tenant_id):
    """
    Retrieve the list of user team departments.

    Query Params:
        parent_id (str):
            - If omitted or empty, returns the root departments associated with the user.
            - If provided, returns the sub-departments under the specified parent department.
        all (bool):
            - If true, retrieves all departments regardless of their hierarchical structure.
            - This option is effective only for team owners.

    Returns:
        JSON response:
            A list of departments. Each item includes:
                - department_id
                - name
                - path
                - avatar
                - owner_id
                - tenant_id
                - parent_id
                - description
                - update_date
                - delta_seconds
                - formatted_path
                - formatted_path_segments
    """
    if not tenant_id:
        return get_data_error_result(message="Missing required filed `tenant_id`")

    try:
        member = UserTenantService.filter_by_tenant_and_user_id(tenant_id=tenant_id, user_id=current_user.id)
        if not member:
            return get_data_error_result(message="Unrecognized identification.")

        parent_id = request.args.get("parent_id", None)
        if not parent_id or parent_id in ["", '""']:
            parent_id = None

        is_owner = current_user.id == tenant_id
        if is_owner:
            fetch_all = request.args.get("all", False)
            departments = DepartmentService.get_subdepartments_by_tenant_id(tenant_id=tenant_id, parent_id=parent_id, fetch_all=fetch_all)
        else:
            departments = DepartmentService.get_subdepartments_by_tenant_id(tenant_id=tenant_id, parent_id=parent_id)

        for department in departments:
            department["delta_seconds"] = delta_seconds(str(department["update_date"]))
            department["formatted_path_segments"] = department["formatted_path"].strip("/").split("/")
        return get_json_result(data=departments)
    except Exception as e:
        return server_error_response(e)


@manager.route("/department/create", methods=["POST"])  # noqa: F821
@login_required
@team_role_guard
@validate_request("name")  # noqa: F821
async def create_department():
    """
    Create a new team department.

    Request Body:
        {
            "name": "A subdepartment",
            "avatar": "",
            "parent_id": "67bed903548836545cdf3a7f",
            "description": ""
        }

    Returns:
        JSON: Success message with department details
    """
    req = await request.get_json()
    name = req.get("name")
    avatar = req.get("avatar", "")
    parent_id = req.get("parent_id")
    description = req.get("description", "")
    tenant_id = current_user.id

    if not name:
        return get_data_error_result(message="Missing required fields: `name`.")

    owner = UserTenantService.filter_by_tenant_and_user_id(current_user.id, current_user.id)
    if not owner:
        return get_data_error_result(message="Unrecognized identification.")
    owner_id = owner.id

    parent_path = ""
    parent_formatted_path = ""
    if parent_id:
        parent = DepartmentService.filter_by_id(parent_id)
        if not parent:
            return get_data_error_result(message=f"Parent department `{parent_id}` does not exist.")
        if parent.tenant_id != tenant_id:
            return get_json_result(data=False, message="No permission to access the parent department.", code=RetCode.PERMISSION_ERROR)
        parent_path = parent.path
        parent_formatted_path = parent.formatted_path

    department_id = get_uuid()
    parent_path = parent_path if parent_path else "/"
    parent_id = parent_id if parent_id else department_id
    parent_formatted_path = parent_formatted_path if parent_formatted_path else "/"

    try:
        with DB.atomic():
            department = {
                "id": department_id,
                "name": name,
                "description": description,
                "avatar": avatar,
                "path": parent_path + department_id + "/",
                "formatted_path": parent_formatted_path + name + "/",
                "parent_id": parent_id,
                "owner_id": owner_id,
                "tenant_id": tenant_id,
            }
            DepartmentService.save(**department)

            department["formatted_path_segments"] = department["formatted_path"].strip("/").split("/")
            return get_json_result(data=department)

    except Exception as e:
        server_error_response(e)


@manager.route("/department/delete/<department_id>", methods=["DELETE"])  # noqa: F821
@login_required
@team_role_guard
def delete_department(department_id):
    """
    Delete a specific team department.

    Query Parameters:
        department_id (string): The ID of the department to be deleted

    Returns:
        JSON: Success message confirming deletion
    """
    tenant_id = current_user.id
    department = DepartmentService.filter_by_id(department_id)
    if not department:
        return get_data_error_result(message=f"Department `{department_id}` does not exist.")

    owner = UserTenantService.filter_by_tenant_and_user_id(current_user.id, current_user.id)
    if not owner:
        return get_data_error_result(message="Unrecognized identification.")
    owner_id = owner.id

    if department.owner_id != owner_id:
        return get_json_result(data=False, message="Permission denied", code=RetCode.PERMISSION_ERROR)

    def get_all_subdepartment_ids(department_id, tenant_id):
        sub_departments = DepartmentService.get_subdepartments_by_tenant_id(tenant_id=tenant_id, parent_id=department_id)

        all_subdepartment_ids = set()

        for sub_department in sub_departments:
            all_subdepartment_ids.add(sub_department["department_id"])
            all_subdepartment_ids.update(get_all_subdepartment_ids(sub_department["department_id"], tenant_id))

        return all_subdepartment_ids

    member_model_list = DepartmentMemberService.get_by_department_id(department_id)
    department_ids = {department_id}
    department_ids.update(get_all_subdepartment_ids(department_id, current_user.id))
    department_ids = list(department_ids)

    try:
        with DB.atomic():
            DepartmentMemberService.delete(member_model_list)
            DepartmentService.delete_departments(department_ids)

            for department_id in department_ids:
                permissions = PermissionService.get_permissions_by_tenant_and_department_id_with_types(tenant_id=tenant_id, department_id=department_id)
                PermissionService.delete(permissions)

        return get_json_result(data=True)

    except Exception as e:
        server_error_response(e)


@manager.route("/department/move", methods=["PUT"])  # noqa: F821
@login_required
@team_role_guard
@validate_request("department_id", "parent_id")  # noqa: F821
async def move_department():
    req = await request.get_json()
    department_id = req.get("department_id")
    if not department_id:
        return get_data_error_result(message="Missing required field `department_id`.")
    parent_id = req.get("parent_id")
    if not parent_id:
        return get_data_error_result(message="Missing required field `parent_id`.")

    move_to_root = department_id == parent_id

    department = DepartmentService.filter_by_id(department_id)
    if not department:
        return get_data_error_result(message=f"Department `{department_id}` does not exist.")
    owner = department.owner_id

    operator = UserTenantService.filter_by_tenant_and_user_id(tenant_id=current_user.id, user_id=current_user.id)
    if not operator or operator.id != owner:
        return get_data_error_result(message="Unrecognized identification.")

    if not move_to_root:
        parent_department = DepartmentService.filter_by_id(parent_id)
        if not parent_department:
            return get_data_error_result(message=f"Parent department `{parent_id}` does not exist.")
        department.path = parent_department.path + department.id + "/"
        department.formatted_path = parent_department.formatted_path + department.name + "/"
        department.parent_id = parent_department.id
    else:
        department.parent_id = department_id
        department.path = department_id + "/"
        department.formatted_path = department.name + "/"

    with DB.atomic():
        DepartmentService.update_department_model(department_model=department)

    return get_json_result(data=True)


@manager.route("<tenant_id>/department/update", methods=["PUT"])  # noqa: F821
@login_required
@team_role_guard
@validate_request("department_id")  # noqa: F821
async def update_department(tenant_id):
    """
    Update department information, including the member list and roles.
    Allowed to update fields: ["name", "avatar", "status", "description"]
    Allowed to update department member fields: ["department_id", "role", "status"]
    Member_list is optional

    Request Body:
        {
            "department_id": "67c47bd8f2e078c384fe4a72",
            "name": "Updated Department Name",
            "avatar": "updated_avatar_url",
            "description": "Updated description",
            "member_list": [
                {
                    "member_id": "67bed779548836545cdf37b5",
                    "role": "admin"
                },
                {
                    "member_id": "67bed99cf57aa5b4c309e305",
                    "role": "member"
                }
            ]
        }

    Returns:
        JSON: Success message confirming update
    """
    req = await request.get_json()
    department_id = req.get("department_id")
    if not department_id:
        return get_data_error_result(message="Missing required field `department_id`.")

    department = DepartmentService.filter_by_id(department_id)
    if not department:
        return get_data_error_result(message=f"Department `{department_id}` does not exist.")
    department_owner = department.owner_id

    operator = UserTenantService.filter_by_tenant_and_user_id(tenant_id=tenant_id, user_id=current_user.id)
    if not operator or operator.id != department_owner:
        return get_data_error_result(message="Unrecognized identification.")

    # department_operator = DepartmentMemberService.filter_by_department_and_member_id(department_id=department_id, member_id=operator.id)
    # if not department_operator:
    #     return get_data_error_result(message="Unrecognized department identification.")

    # if department_operator.role not in MANAGEMENT_TEAM_ROLES:
    #     return get_json_result(
    #         data=False,
    #         message="Permission denied",
    #         code=RetCode.PERMISSION_ERROR)

    allow_to_update = ["name", "avatar", "description", "status"]
    department_dict = {key: req[key] for key in req if key in allow_to_update}

    member_allow_to_update = ["department_id", "role", "status"]
    members_to_update = []
    members_to_insert = []
    members_to_delete = []
    member_list = req.get("member_list", [])
    if member_list:
        for member_info in req["member_list"]:
            is_deletion = False
            if "role" in member_info and "member_id" in member_info:
                member_id = member_info["member_id"]
                role = member_info["role"]
                if (role not in VALID_TEAM_ROLES) or (role == TeamRole.OWNER and member_id != department_owner):
                    continue

                member = DepartmentMemberService.filter_by_department_and_member_id(department_id, member_id)
                if not member:
                    if not UserTenantService.filter_by_id(member_id):
                        continue
                    member_info["department_id"] = department_id
                    members_to_insert.append(member_info)
                    continue

                for field in member_allow_to_update:
                    if field in member_info:
                        if field == "role" and member.role == TeamRole.OWNER:
                            continue
                        if field == "status" and member.status == StatusEnum.INVALID.value:
                            is_deletion = True
                        setattr(member, field, member_info[field])
                if is_deletion:
                    members_to_delete.append(member)
                else:
                    members_to_update.append(member)

    try:
        with DB.atomic():
            DepartmentService.update_department(department_id, department_dict)

            if members_to_insert:
                DepartmentMemberService.insert_many(members_to_insert)

            if members_to_update:
                DepartmentMemberService.update_many(members_to_update, member_allow_to_update)

            if members_to_delete:
                DepartmentMemberService.update_many(members_to_delete, member_allow_to_update)

                for member in members_to_delete:
                    permissions = PermissionService.get_permissions_by_tenant_and_member_id_with_types(tenant_id=tenant_id, member_id=member.id)
                    PermissionService.delete(permissions)

        return get_json_result(data=True)
    except Exception as e:
        server_error_response(e)


@manager.route("<tenant_id>/department/members/<department_id>", methods=["GET"])  # noqa: F821
@login_required
@team_role_guard
def list_department_member(tenant_id, department_id):
    """
    List members of specific department
    """
    department = DepartmentService.filter_by_id(department_id)
    if not department:
        return get_data_error_result(message=f"Department `{department_id}` does not exist.")
    # department_owner = department.owner_id

    checker = UserTenantService.filter_by_tenant_and_user_id(tenant_id=tenant_id, user_id=current_user.id)
    if not checker:
        return get_data_error_result(message="Unrecognized identification.")

    # is_team_owner = tenant_id == current_user.id
    #
    # if not is_team_owner:
    #     if checker.id != department_owner:
    #         if not DepartmentMemberService.filter_by_department_and_member_id(department_id=department_id, member_id=checker.id):
    #             return get_data_error_result(message="Unrecognized department identification.")

    department_list = DepartmentMemberService.get_by_department_id_with_info(department_id)

    return get_json_result(data=department_list)


@manager.route("<tenant_id>/department/member/create", methods=["POST"])  # noqa: F821
@login_required
@team_role_guard
@validate_request("department_id", "member_list")  # noqa: F821
async def add_department_member(tenant_id):
    """
    Add multiple members to a department.

    Request Body:
        {
            "department_id": "67c47bd8f2e078c384fe4a72",
            "member_list": [
                {
                    "member_id": "67bed779548836545cdf37b5",
                    "role": "admin"
                },
                {
                    "member_id": "67bed779548836545cdf37b6",
                    "role": "member"
                }
            ]
        }

    Returns:
        JSON: Success message confirming addition
    """
    req = await request.get_json()
    department_id = req.get("department_id")
    member_list = req.get("member_list", [])

    if not department_id or not member_list:
        return get_data_error_result(message="Missing required fields: `department_id`, `member_list`.")

    d = DepartmentService.filter_by_id(department_id)
    if not d:
        return get_data_error_result(message=f"Department `{department_id}` does not exist.")
    owner = d.owner_id

    operator = UserTenantService.filter_by_tenant_and_user_id(tenant_id=tenant_id, user_id=current_user.id)
    if not operator or operator.id != owner:
        return get_data_error_result(message="Unrecognized identification.")

    # department_operator = DepartmentMemberService.filter_by_department_and_member_id(department_id=department_id, member_id=operator.id)
    # if not department_operator:
    #     return get_data_error_result(message="Unrecognized department identification.")

    # if department_operator.role not in MANAGEMENT_TEAM_ROLES:
    #     return get_json_result(
    #         data=False,
    #         message="Permission denied",
    #         code=RetCode.PERMISSION_ERROR)

    not_added = []
    members = []
    for member_info in member_list:
        if "role" in member_info and "member_id" in member_info:
            member_id = member_info["member_id"]
            role = member_info["role"]

            if (role not in VALID_TEAM_ROLES) or (role == TeamRole.OWNER) or (member_id == owner):
                not_added.append(member_id)
                continue

            if not UserTenantService.filter_by_id(member_id):
                not_added.append(member_id)
                continue

            if DepartmentMemberService.filter_by_department_and_member_id(department_id, member_id):
                not_added.append(member_id)
                continue

            member = {"id": get_uuid(), "department_id": department_id, "member_id": member_id, "role": role}
            members.append(member)

    try:
        with DB.atomic():
            DepartmentMemberService.insert_many(members)

            return get_json_result(data={"department_id": department_id, "members_added": members, "members_not_added": not_added})

    except Exception as e:
        server_error_response(e)


@manager.route("<tenant_id>/department/member/delete", methods=["DELETE"])  # noqa: F821
@login_required
@team_role_guard
@validate_request("department_id", "member_list")  # noqa: F821
async def remove_department_member(tenant_id):
    """
    Delete multiple members to a department.

    Request Body:
        {
            "department_id": "67c47bd8f2e078c384fe4a72",
            "member_list": [
                {
                    "member_id": "67bed779548836545cdf37b5",
                    "role": "admin"
                },
                {
                    "member_id": "67bed779548836545cdf37b6",
                    "role": "member"
                }
            ]
        }

    Returns:
        JSON: Success message confirming addition
    """
    req = await request.get_json()
    department_id = req.get("department_id")
    member_list = req.get("member_list", [])

    if not department_id or not member_list:
        return get_data_error_result(message="Missing required fields: `department_id`, `member_list`.")

    d = DepartmentService.filter_by_id(department_id)
    if not d:
        return get_data_error_result(message=f"Department `{department_id}` does not exist.")
    owner = d.owner_id

    operator = UserTenantService.filter_by_tenant_and_user_id(tenant_id=tenant_id, user_id=current_user.id)
    if not operator or operator.id != owner:
        return get_data_error_result(message="Unrecognized identification.")

    # department_operator = DepartmentMemberService.filter_by_department_and_member_id(department_id=department_id, member_id=operator.id)
    # if not department_operator:
    #     return get_data_error_result(message="Unrecognized department identification.")

    # if department_operator.role not in MANAGEMENT_TEAM_ROLES:
    #     return get_json_result(
    #         data=False,
    #         message="Permission denied",
    #         code=RetCode.PERMISSION_ERROR)

    not_deleted = []
    member_ids = []
    for member_info in member_list:
        if "role" in member_info and "member_id" in member_info:
            member_id = member_info["member_id"]
            role = member_info["role"]

            if (role not in VALID_TEAM_ROLES) or (role == TeamRole.OWNER):
                not_deleted.append(member_id)
                continue

            member_ids.append(member_id)

    members = DepartmentMemberService.filter_by_group_and_member_ids(department_id, member_ids)
    try:
        with DB.atomic():
            if members:
                DepartmentMemberService.delete(members)

                for member in members:
                    permissions = PermissionService.get_permissions_by_tenant_and_member_id_with_types(tenant_id=tenant_id, member_id=member.member_id)
                    PermissionService.delete(permissions)

        return get_json_result(data={"department_id": department_id, "members_deleted": member_ids, "members_not_deleted": not_deleted})
    except Exception as e:
        server_error_response(e)
