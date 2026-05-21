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

from enum import IntEnum, Enum
from enum import StrEnum


class PermissionOperationEnum(StrEnum):
    GRANT = 'grant'
    REVOKE = 'revoke'


class ResourceTypeEnum(Enum):
    # 0 is reserved
    DATASET = 1
    CHAT = 2
    AGENT = 3
    SEARCH = 4
    FILE = 5
    TEAM = 6
    MEMORY = 7
    MODEL_PROVIDER = 8


class ActionEnum(Enum):
    ENABLE = 0b0001  # 1 << 0 = 1 (0b00000001)
    READ = 0b0010    # 1 << 1 = 2 (0b00000010)
    WRITE = 0b0100   # 1 << 2 = 4 (0b00000100)
    SHARE = 0b1000   # 1 << 3 = 8 (0b00001000)


class RoleDefaultModelSetUpStatusEnum(StrEnum):
    NOT_SET = "not_set"
    PARTIAL = "partial"
    COMPLETE = "complete"


class UserTenantRole(StrEnum):
    OWNER = "owner"
    ADMIN = "admin"
    NORMAL = "normal"
    INVITE = "invite"


class TenantPermission(StrEnum):
    ME = "me"
    TEAM = "team"


class TeamRole(StrEnum):
    OWNER = "owner"
    ADMIN = "admin"
    MEMBER = "member"


VALID_TEAM_ROLES = ["owner", "admin", "member"]
MANAGEMENT_TEAM_ROLES = ["owner", "admin"]


class ResourceType(StrEnum):
    KB = "kb"
    LLM = "llm"
    TEAM = "team"
    DIALOG = "dialog"
    DOCUMENT = "document"
    CANVAS = "canvas"
    MCP = "mcp"


VALID_RESOURCE_TYPES = ["kb", "llm", "team", "dialog", "document", "canvas", "mcp"]


class PermissionActionType(StrEnum):
    ACTION_ADD = "add"
    ACTION_UPDATE = "update"
    ACTION_DELETE = "delete"
    ACTION_REVOKE = "revoke"


VALID_PERMISSION_ACTION_TYPES = ["add", "update", "delete", "revoke"]


class PermissionTargetType(StrEnum):
    TARGET_MEMBER = "member"
    TARGET_GROUP = "group"
    TARGET_DEPARTMENT = "department"


VALID_PERMISSION_TARGET_TYPES = ["member", "group", "department"]


class PermissionValue(Enum):
    PERMISSION_NULL = 0b000  # 0
    PERMISSION_READ = 0b001  # 1
    PERMISSION_WRITE = 0b010  # 2
    PERMISSION_MANAGE = 0b100  # 4
    PERMISSION_OWNER = 0b111  # 7


class SerializedType(IntEnum):
    PICKLE = 1
    JSON = 2


class FileType(StrEnum):
    PDF = "pdf"
    DOC = "doc"
    VISUAL = "visual"
    AURAL = "aural"
    VIRTUAL = "virtual"
    FOLDER = "folder"
    OTHER = "other"


VALID_FILE_TYPES = {FileType.PDF, FileType.DOC, FileType.VISUAL, FileType.AURAL, FileType.VIRTUAL, FileType.FOLDER, FileType.OTHER}


class InputType(StrEnum):
    LOAD_STATE = "load_state"  # e.g. loading a current full state or a save state, such as from a file
    POLL = "poll"  # e.g. calling an API to get all documents in the last hour
    EVENT = "event"  # e.g. registered an endpoint as a listener, and processing connector events
    SLIM_RETRIEVAL = "slim_retrieval"


class CanvasCategory(StrEnum):
    Agent = "agent_canvas"
    DataFlow = "dataflow_canvas"


class PipelineTaskType(StrEnum):
    PARSE = "Parse"
    DOWNLOAD = "Download"
    RAPTOR = "RAPTOR"
    GRAPH_RAG = "GraphRAG"
    MINDMAP = "Mindmap"


VALID_PIPELINE_TASK_TYPES = {PipelineTaskType.PARSE, PipelineTaskType.DOWNLOAD, PipelineTaskType.RAPTOR, PipelineTaskType.GRAPH_RAG, PipelineTaskType.MINDMAP}


################# billing
class ProductType(StrEnum):
    """Types of products"""

    SUBSCRIPTION = "subscription"
    ADDON = "addon"


class UsageStat(StrEnum):
    """Usage statistic timing"""

    BEFORE_TASK = "before"
    AFTER_TASK = "after"


class QuotaType(StrEnum):
    """Quota definition types"""

    APP_TOTAL = "app_total"
    TEAM_SEAT = "team_seat"
    API_QPS = "api_qps"
    KB_STORAGE = "kb_storage"


class QuotaUnit(StrEnum):
    """Units for quota items"""

    APPS = "apps"
    SEATS = "seats"
    CALLS = "calls"
    GB = "gb"


class PriceType(StrEnum):
    """Types of pricing methods"""

    SUBSCRIPTION = "subscription"
    ADDON = "addon"


class BillingFrequency(StrEnum):
    """Billing intervals"""

    MONTHLY = "monthly"
    YEARLY = "yearly"
    ONE_TIME = "one_time"


class PriceUnit(StrEnum):
    """Units for usage-based pricing"""

    TOKEN = "token"
    PAGE = "page"


class PaymentCurrency(StrEnum):
    USD = "usd"
    CNY = "cny"


class ProductTaskStatus(StrEnum):
    """Product task status"""

    WAITING = "waiting"
    SUCCESS = "success"
    FAILED = "failed"


class PaymentMethod(StrEnum):
    CARD = "card"


class PaymentChannel(StrEnum):
    STRIPE = "stripe"


class PaymentStatus(StrEnum):
    PENDING = "pending"
    SUCCESS = "success"
    FAILED = "failed"


class UsageBasedStatus(StrEnum):
    PENDING = "pending"
    ACTIVE = "active"
    CANCELED = "canceled"
    EXPIRED = "expired"


class UsageTraceStatus(StrEnum):
    WAITING = "waiting"
    SUCCESS = "success"
    FAILED = "failed"


class SubscriptionStatus(StrEnum):
    INACTIVE = "inactive"
    ACTIVE = "active"
    CANCELED = "canceled"
    EXPIRED = "expired"
    PENDING = "pending"
    UNKNOWN = "unknown"


class PaygStatus(StrEnum):
    DISABLED = "disabled"
    PENDING = "pending"
    ACTIVE = "active"
    GRACE = "grace"
    CANCELING = "canceling"


####################

PIPELINE_SPECIAL_PROGRESS_FREEZE_TASK_TYPES = {PipelineTaskType.RAPTOR.lower(), PipelineTaskType.GRAPH_RAG.lower(), PipelineTaskType.MINDMAP.lower()}


KNOWLEDGEBASE_FOLDER_NAME=".knowledgebase"
SKILLS_FOLDER_NAME="skills"
