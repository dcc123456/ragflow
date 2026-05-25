from api.db.services.system_settings_service import SystemSettingsService
from api.utils.api_utils import get_json_result
from api.utils.system_settings_utils import load_value_from_string

NOTIFICATION_FIELDS = {
    "notification.content": "content",
    "notification.enabled": "enabled"
}


@manager.route("/notification", methods=["GET"])  # noqa: F821
def get_notification():
    notification_rows = SystemSettingsService.get_by_name_prefix("notification.")
    result = {}
    for row in notification_rows:
        key = NOTIFICATION_FIELDS.get(row.name)
        if key:
            result[key] = load_value_from_string(row.value, row.data_type)
    return get_json_result(data=result)
