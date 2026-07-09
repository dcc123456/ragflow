import logging
import time
from typing import Union, List

from rag.nlp import rag_tokenizer


SENSITIVE_WORD_FILTER_ENABLED = False
SENSITIVE_WORD_FILTER_FILE_BUCKET = "ragflow-system"
SENSITIVE_WORD_FILTER_FILE_NAME = "sensitive_word_filter/compliance.txt"
SENSITIVE_WORD_FILTER_FILE_TENANT_ID = None
SENSITIVE_WORD_FILTER_FILE_VERSION_SETTING = "sensitive_word_filter.file_version"
SENSITIVE_WORD_FILTER_ENABLED_SETTING = "sensitive_word_filter.enabled"
SENSITIVE_WORD_FILTER_RESPONSE_SETTING = "sensitive_word_filter.response"
DEFAULT_SENSITIVE_QUESTION_RESPONSE = "Your request could not be processed because it contains restricted content. Please modify your input and try again."
SENSITIVE_QUESTION_RESPONSE = DEFAULT_SENSITIVE_QUESTION_RESPONSE
_SENSITIVE_WORD_FILTER_FILE_VERSION = None
_SENSITIVE_WORD_FILTER_SETTINGS_LOADED = False
_RELOAD_INTERVAL_SECONDS = 30
_NEXT_RELOAD_AT = 0


class Dealer:
    def __init__(self):
        self.dictionary = None

    def load(self, content: str):
        self.dictionary = set([line.strip().lower() for line in content.splitlines() if line.strip()])

    def clear(self):
        self.dictionary = None

    def lookup(self, txt: Union[str, List]) -> List:
        if not self.dictionary:
            return []

        tks = txt if isinstance(txt, list) else rag_tokenizer.tokenize(txt).split(" ")
        for j in range(1, 6):
            s = set(["".join([tks[i + ii] for ii in range(j)]) for i in range(len(tks) - j + 1)]) | set([" ".join([tks[i + ii] for ii in range(j)]) for i in range(len(tks) - j + 1)])
            comm = set(s) & self.dictionary
            if comm:
                return list(comm)

            if len(tks) < j + 1:
                return []
        return []


COMPLIANCE = Dealer()


def _load_setting(name: str):
    try:
        from api.db.services.system_settings_service import SystemSettingsService
        from api.utils.system_settings_utils import load_value_from_string

        record = SystemSettingsService.get_singleton_by_exact_name(name)
        if not record:
            return None
        return load_value_from_string(record.value, record.data_type)
    except Exception as e:
        logging.debug("Failed to load sensitive word filter setting %s: %s", name, e)
        return None


def put_sensitive_word_file(content: bytes):
    from common import settings

    return settings.STORAGE_IMPL.put(SENSITIVE_WORD_FILTER_FILE_BUCKET, SENSITIVE_WORD_FILTER_FILE_NAME, content, SENSITIVE_WORD_FILTER_FILE_TENANT_ID)


def get_sensitive_word_file():
    from common import settings

    try:
        return settings.STORAGE_IMPL.get(SENSITIVE_WORD_FILTER_FILE_BUCKET, SENSITIVE_WORD_FILTER_FILE_NAME, SENSITIVE_WORD_FILTER_FILE_TENANT_ID)
    except TypeError:
        return settings.STORAGE_IMPL.get(SENSITIVE_WORD_FILTER_FILE_BUCKET, SENSITIVE_WORD_FILTER_FILE_NAME)


def sensitive_word_file_exists():
    from common import settings

    try:
        return settings.STORAGE_IMPL.obj_exist(SENSITIVE_WORD_FILTER_FILE_BUCKET, SENSITIVE_WORD_FILTER_FILE_NAME, SENSITIVE_WORD_FILTER_FILE_TENANT_ID)
    except TypeError:
        return settings.STORAGE_IMPL.obj_exist(SENSITIVE_WORD_FILTER_FILE_BUCKET, SENSITIVE_WORD_FILTER_FILE_NAME)


def reload_sensitive_word_filter():
    global SENSITIVE_WORD_FILTER_ENABLED, SENSITIVE_QUESTION_RESPONSE, _SENSITIVE_WORD_FILTER_FILE_VERSION, _SENSITIVE_WORD_FILTER_SETTINGS_LOADED, _NEXT_RELOAD_AT
    if time.monotonic() < _NEXT_RELOAD_AT:
        return
    _NEXT_RELOAD_AT = time.monotonic() + _RELOAD_INTERVAL_SECONDS
    enabled = _load_setting(SENSITIVE_WORD_FILTER_ENABLED_SETTING)
    SENSITIVE_WORD_FILTER_ENABLED = bool(enabled)
    if not SENSITIVE_WORD_FILTER_ENABLED:
        COMPLIANCE.clear()
        _SENSITIVE_WORD_FILTER_SETTINGS_LOADED = False
        _SENSITIVE_WORD_FILTER_FILE_VERSION = None
        return

    response = _load_setting(SENSITIVE_WORD_FILTER_RESPONSE_SETTING)
    SENSITIVE_QUESTION_RESPONSE = response if isinstance(response, str) and response else DEFAULT_SENSITIVE_QUESTION_RESPONSE
    file_version = _load_setting(SENSITIVE_WORD_FILTER_FILE_VERSION_SETTING)
    file_version = str(file_version or "")
    if not _SENSITIVE_WORD_FILTER_SETTINGS_LOADED or file_version != _SENSITIVE_WORD_FILTER_FILE_VERSION:
        try:
            content = get_sensitive_word_file()
        except Exception as e:
            logging.warning("Failed to load sensitive word filter file from storage: %s", e)
            content = None
        if content:
            try:
                COMPLIANCE.load(content.decode("utf-8-sig"))
                _SENSITIVE_WORD_FILTER_SETTINGS_LOADED = True
                _SENSITIVE_WORD_FILTER_FILE_VERSION = file_version
            except UnicodeDecodeError as e:
                logging.warning("Failed to load sensitive word filter file to memory: %s", e)


def sensitive_question_words(question: str) -> List:
    reload_sensitive_word_filter()
    if not SENSITIVE_WORD_FILTER_ENABLED:
        return []
    return COMPLIANCE.lookup(question or "")
