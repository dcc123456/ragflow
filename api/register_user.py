import argparse
from common import settings
from api.db.services.user_service import UserService

from api.db.services.tenant_llm_service import user_register
from common.time_utils import get_format_time, current_timestamp
from common.misc_utils import get_uuid

settings.init_settings()


def create(email):
    users = UserService.query(email=email)
    if users:
        user = users[0]
        user.status = "1"
        UserService.update_by_id(user.id, user.to_dict())
        return
    user_register(
        get_uuid(),
        {
            "access_token": get_uuid(),
            "email": email,
            "nickname": email.split("@")[0],
            "login_channel": "entraID",
            "last_login_time": get_format_time(),
            "update_time": current_timestamp(),
            "language": "Chinese",
        },
    )


def disable(email):
    users = UserService.query(email=email)
    assert users, "Failure. E-mail does not exist."
    user = users[0]
    user.status = "0"
    UserService.update_by_id(user.id, user.to_dict())


if __name__ == "__main__":
    parser = argparse.ArgumentParser(prog="User control", description="Create/disable a user")
    parser.add_argument("-e", "--email", type=str, required=True)
    parser.add_argument("-o", "--operation", type=str, default="create", help="[create, disable]")
    args = parser.parse_args()
    email = args.email
    operation = args.operation
    if operation == "create":
        create(email)
        print("Done")
    elif operation == "disable":
        disable(email)
        print("Done")
    else:
        assert False, "Operation should be in `create`/`disable`."
