from tqdm import tqdm

import time
from peewee import CharField, TextField, BooleanField, Model, IntegerField, FloatField
from playhouse.pool import PooledMySQLDatabase

from api.db import FileType, UserTenantRole
from api.db.services.file_service import FileService
from api.db.services.llm_service import TenantLLMService, LLMService
from api.db.services import UserService
from api.db.services.user_service import UserTenantService, TenantService
from common.time_utils import get_format_time, current_timestamp
from common.misc_utils import get_uuid
from common import settings

casdoor = PooledMySQLDatabase(
    'casdoor',
    max_connections=8,
    stale_timeout=300,
    user='root',
    password='cc0hfUC84uppmJBgFVDU',
    host='10.138.0.5',
    port=33061,
    charset='utf8mb4')

settings.init_settings()

class CasdoorUser(Model):
    owner = CharField(max_length=100, null=False, index=True)
    name = CharField(max_length=100, null=False, index=True)
    created_time = CharField(max_length=100, null=True, index=False)
    updated_time = CharField(max_length=100, null=True, index=False)
    deleted_time = CharField(max_length=100, null=True, index=False)
    id = CharField(max_length=100, null=True, index=False)
    external_id = CharField(max_length=100, null=True, index=False)
    type = CharField(max_length=100, null=True, index=False)
    password = CharField(max_length=150, null=False)
    password_salt = CharField(max_length=100, null=True, index=False)
    password_type = CharField(max_length=100, null=True, index=False)
    display_name = CharField(max_length=100, null=True, index=False)
    first_name = CharField(max_length=100, null=True, index=False)
    last_name = CharField(max_length=100, null=True, index=False)
    avatar = TextField(null=True)
    avatar_type = CharField(max_length=100, null=True, index=False)
    permanent_avatar = CharField(max_length=500, null=False)
    email = CharField(max_length=100, null=True, index=False)
    email_verified = BooleanField(null=True)
    phone = CharField(max_length=100, null=True, index=False)
    country_code = CharField(max_length=6, null=False)
    region = CharField(max_length=100, null=True, index=False)
    location = CharField(max_length=100, null=True, index=False)
    address = TextField(null=True)
    affiliation = CharField(max_length=100, null=True, index=False)
    title = CharField(max_length=100, null=True, index=False)
    id_card_type = CharField(max_length=100, null=True, index=False)
    id_card = CharField(max_length=100, null=True, index=False)
    homepage = CharField(max_length=100, null=True, index=False)
    bio = CharField(max_length=100, null=True, index=False)
    tag = CharField(max_length=100, null=True, index=False)
    language = CharField(max_length=100, null=True, index=False)
    gender = CharField(max_length=100, null=True, index=False)
    birthday = CharField(max_length=100, null=True, index=False)
    education = CharField(max_length=100, null=True, index=False)
    score = IntegerField(index=False, null=True)
    karma = IntegerField(index=False, null=True)
    ranking = IntegerField(index=False, null=True)
    balance = FloatField(index=True, null=True)
    currency = CharField(max_length=100, null=True, index=False)
    is_default_avatar = BooleanField(null=True)
    is_online = BooleanField(null=True)
    is_admin = BooleanField(null=True)
    is_forbidden = BooleanField(null=True)
    is_deleted = BooleanField(null=True)
    signup_application = CharField(max_length=100, null=True, index=False)
    hash = CharField(max_length=100, null=True, index=False)
    pre_hash = CharField(max_length=100, null=True, index=False)
    access_key = CharField(max_length=100, null=True, index=False)
    access_secret = CharField(max_length=100, null=True, index=False)
    access_token = TextField(null=True)
    created_ip = CharField(max_length=100, null=True, index=False)
    last_signin_time = CharField(max_length=100, null=True, index=False)
    last_signin_ip = CharField(max_length=100, null=True, index=False)
    github = CharField(max_length=100, null=True, index=False)
    google = CharField(max_length=100, null=True, index=False)
    qq = CharField(max_length=100, null=True, index=False)
    wechat = CharField(max_length=100, null=True, index=False)
    facebook = CharField(max_length=100, null=True, index=False)
    dingtalk = CharField(max_length=100, null=True, index=False)
    weibo = CharField(max_length=100, null=True, index=False)
    gitee = CharField(max_length=100, null=True, index=False)
    linkedin = CharField(max_length=100, null=True, index=False)
    wecom = CharField(max_length=100, null=True, index=False)
    lark = CharField(max_length=100, null=True, index=False)
    gitlab = CharField(max_length=100, null=True, index=False)
    adfs = CharField(max_length=100, null=True, index=False)
    baidu = CharField(max_length=100, null=True, index=False)
    alipay = CharField(max_length=100, null=True, index=False)
    casdoor = CharField(max_length=100, null=True, index=False)
    infoflow = CharField(max_length=100, null=True, index=False)
    apple = CharField(max_length=100, null=True, index=False)
    azuread = CharField(max_length=100, null=True, index=False)
    azureadb2c = CharField(max_length=100, null=True, index=False)
    slack = CharField(max_length=100, null=True, index=False)
    steam = CharField(max_length=100, null=True, index=False)
    bilibili = CharField(max_length=100, null=True, index=False)
    okta = CharField(max_length=100, null=True, index=False)
    douyin = CharField(max_length=100, null=True, index=False)
    kwai = CharField(max_length=100, null=True, index=False)
    line = CharField(max_length=100, null=True, index=False)
    amazon = CharField(max_length=100, null=True, index=False)
    auth0 = CharField(max_length=100, null=True, index=False)
    battlenet = CharField(max_length=100, null=True, index=False)
    bitbucket = CharField(max_length=100, null=True, index=False)
    box = CharField(max_length=100, null=True, index=False)
    cloudfoundry = CharField(max_length=100, null=True, index=False)
    dailymotion = CharField(max_length=100, null=True, index=False)
    deezer = CharField(max_length=100, null=True, index=False)
    digitalocean = CharField(max_length=100, null=True, index=False)
    discord = CharField(max_length=100, null=True, index=False)
    dropbox = CharField(max_length=100, null=True, index=False)
    eveonline = CharField(max_length=100, null=True, index=False)
    fitbit = CharField(max_length=100, null=True, index=False)
    gitea = CharField(max_length=100, null=True, index=False)
    heroku = CharField(max_length=100, null=True, index=False)
    influxcloud = CharField(max_length=100, null=True, index=False)
    instagram = CharField(max_length=100, null=True, index=False)
    intercom = CharField(max_length=100, null=True, index=False)
    kakao = CharField(max_length=100, null=True, index=False)
    lastfm = CharField(max_length=100, null=True, index=False)
    mailru = CharField(max_length=100, null=True, index=False)
    meetup = CharField(max_length=100, null=True, index=False)
    microsoftonline = CharField(max_length=100, null=True, index=False)
    naver = CharField(max_length=100, null=True, index=False)
    nextcloud = CharField(max_length=100, null=True, index=False)
    onedrive = CharField(max_length=100, null=True, index=False)
    oura = CharField(max_length=100, null=True, index=False)
    patreon = CharField(max_length=100, null=True, index=False)
    paypal = CharField(max_length=100, null=True, index=False)
    salesforce = CharField(max_length=100, null=True, index=False)
    shopify = CharField(max_length=100, null=True, index=False)
    soundcloud = CharField(max_length=100, null=True, index=False)
    spotify = CharField(max_length=100, null=True, index=False)
    strava = CharField(max_length=100, null=True, index=False)
    stripe = CharField(max_length=100, null=True, index=False)
    tiktok = CharField(max_length=100, null=True, index=False)
    tumblr = CharField(max_length=100, null=True, index=False)
    twitch = CharField(max_length=100, null=True, index=False)
    twitter = CharField(max_length=100, null=True, index=False)
    typetalk = CharField(max_length=100, null=True, index=False)
    uber = CharField(max_length=100, null=True, index=False)
    vk = CharField(max_length=100, null=True, index=False)
    wepay = CharField(max_length=100, null=True, index=False)
    xero = CharField(max_length=100, null=True, index=False)
    yahoo = CharField(max_length=100, null=True, index=False)
    yammer = CharField(max_length=100, null=True, index=False)
    yandex = CharField(max_length=100, null=True, index=False)
    zoom = CharField(max_length=100, null=True, index=False)
    metamask = CharField(max_length=100, null=True, index=False)
    web3onboard = CharField(max_length=100, null=True, index=False)
    custom = CharField(max_length=100, null=True, index=False)
    webauthnCredentials = TextField(null=True)
    preferred_mfa_type = CharField(max_length=100, null=True, index=False)
    recovery_codes = CharField(max_length=1000, null=False)
    totp_secret = CharField(max_length=100, null=True, index=False)
    mfa_phone_enabled = BooleanField(null=True)
    mfa_email_enabled = BooleanField(null=True)
    invitation = CharField(max_length=100, null=True, index=False)
    invitation_code = CharField(max_length=100, null=True, index=False)
    face_ids = TextField(null=True)
    ldap = CharField(max_length=100, null=True, index=False)
    properties = TextField(null=True)
    roles = TextField(null=True)
    permissions = TextField(null=True)
    groups = CharField(max_length=1000, null=False)
    last_change_password_time = CharField(max_length=100, null=True, index=False)
    last_signin_wrong_time = CharField(max_length=100, null=True, index=False)
    signin_wrong_times = IntegerField(index=False, null=True)
    managedAccounts = TextField(null=True)
    mfaAccounts = TextField(null=True)
    need_update_password = BooleanField(null=True)
    ip_whitelist = CharField(max_length=100, null=True, index=False)

    class Meta:
        db_table = "user"
        database = casdoor


def user_register(user_id, user):
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
    tenant_llm = []
    for llm in LLMService.query(fid=settings.LLM_FACTORY):
        tenant_llm.append(
            {
                "tenant_id": user_id,
                "llm_factory": settings.LLM_FACTORY,
                "llm_name": llm.llm_name,
                "model_type": llm.model_type,
                "api_key": settings.API_KEY,
                "api_base": settings.LLM_BASE_URL,
                "max_tokens": llm.max_tokens if llm.max_tokens else 8192
            }
        )
    for buildin_embedding_model in settings.BUILTIN_EMBEDDING_MODELS:
        mdlnm, fid, _ = TenantLLMService.split_model_name_and_factory(buildin_embedding_model)
        tenant_llm.append(
            {
                "tenant_id": user_id,
                "llm_factory": fid,
                "llm_name": mdlnm,
                "model_type": "embedding",
                "api_key": "",
                "api_base": "",
                "max_tokens": 1024 if buildin_embedding_model == "BAAI/bge-large-zh-v1.5@BAAI" else 512,
            }
        )

    if not UserService.save(**user):
        return
    TenantService.insert(**tenant)
    UserTenantService.insert(**usr_tenant)
    TenantLLMService.insert_many(tenant_llm)
    FileService.insert(file)
    time.sleep(3)
    return UserService.query(email=user["email"])


def sync_from_casdoor():
    for u in tqdm(CasdoorUser.select().where(CasdoorUser.created_time> '2025-05-27',CasdoorUser.created_time < '2025-06-27')):
        def is_github():
            nonlocal u
            return (str(u.properties) + u.avatar).lower().find("GitHub") > 0

        users = UserService.query(email=u.email)
        if not users:
            print(f"{u.email} exist....")
            continue

        user_id = get_uuid()
        user_register(
            user_id,
            {
                "access_token": get_uuid(),
                "email": u.email,
                "avatar": u.avatar,
                "nickname": u.display_name if u.display_name else u.name,
                "login_channel": "github" if is_github() else "password",
                "last_login_time": get_format_time(),
                "update_time": current_timestamp(),
                "is_superuser": False,
            },
        )
        break

if __name__ == "__main__":
    try:
        sync_from_casdoor()
    except Exception as e:
        print("【EXCEPTION】: ", e)
