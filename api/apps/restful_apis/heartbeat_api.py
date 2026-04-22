from api.apps import login_required, current_user
from api.utils.api_utils import get_result
from common import active_users


@manager.route("/heartbeat", methods=["GET"])  # noqa: F821
@login_required
def heartbeat():
    """
    Heartbeat, make the user active.
    """
    active_users.mark_user_active(current_user.id)
    return get_result()
