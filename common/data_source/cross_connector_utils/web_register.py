from common.data_source.lark.connector import LarkConnector
from common.data_source.confluence_connector import ConfluenceConnector
from common.data_source.interfaces import StaticCredentialsProvider
from common.data_source.config import DocumentSource
from api.utils.api_utils import get_data_error_result
from api.apps import current_user


def load_lark_connector(connector):
    conf = connector.config
    connector = LarkConnector(conf.get("token_type"), conf.get("token"))

    credentials = conf.get("credentials")
    if not credentials:
        raise get_data_error_result(message="Downloading specific files is not supported by lark connector.")

    connector.load_credentials(credentials)
    connector.validate_connector_settings()

    return connector


def load_confluence_connector(connector):
    conf = connector.config

    connector = ConfluenceConnector(
        wiki_base=conf.get("wiki_base"),
        is_cloud=conf.get("is_cloud", True),
        space=conf.get("space", None),
        page_id=conf.get("page_id", None),
        index_recursively=bool(conf.get("index_recursively", False)),
    )

    credentials_provider = StaticCredentialsProvider(
        tenant_id=current_user.id,
        connector_name=DocumentSource.CONFLUENCE,
        credential_json=conf["credentials"],
    )
    connector.set_credentials_provider(credentials_provider)

    return connector


def lark_build_tree(connector, conn):
    token = conn.config["token"]
    return connector.build_tree(token, "")


def confluence_build_tree(connector, **kwargs):
    return connector.build_tree()


CONNECTOR_REGISTRY = {
    "lark": load_lark_connector,
    "confluence": load_confluence_connector,
}

CONNECTOR_BUILD_TREE = {
    "lark": lark_build_tree,
    "confluence": confluence_build_tree,
}
