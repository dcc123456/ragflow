from common.data_source.lark.connector import LarkConnector
from api.utils.api_utils import get_data_error_result

def load_lark_connector(conf):
    connector = LarkConnector(conf.get("token_type"), conf.get("token"))
    
    credentials = conf.get("credentials")
    if not credentials:
        raise get_data_error_result(message="Downloading specific files is not supported by lark connector.")

    connector.load_credentials(credentials)
    connector.validate_connector_settings()

    return connector


CONNECTOR_REGISTRY = {
    "lark": load_lark_connector
}
