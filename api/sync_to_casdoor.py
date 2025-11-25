import random

from api.db.db_models import User
import time
import sys
from tqdm import tqdm
from peewee import CharField, TextField, BooleanField, Model, IntegerField, FloatField
from playhouse.pool import PooledMySQLDatabase
from datetime import datetime


casdoor = PooledMySQLDatabase(
    'casdoor',
    max_connections=8,
    stale_timeout=300,
    user='root',
    password='cc0hfUC84uppmJBgFVDU',
    host='10.142.0.4',
    port=33061,
    charset='utf8mb4')


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


def get_tm(fnm):
    tm = 0
    try:
        with open(fnm, "r") as f:
            while True:
                ln = f.readline()
                if not ln:
                    break
                try:
                    i = int(ln.strip("\n"))
                except Exception as ee:
                    print(ee)
                    continue
                if i > tm:
                    tm = i
    except Exception as e:
        print("【EXCEPTION】: ", e)
    return tm


def sync2casdoor(dt_fnm):
    from_dt = get_tm(dt_fnm)
    print("[START]: ", from_dt)
    f = (open(dt_fnm, "a+") if random.randint(0, 10) < 5 else open(dt_fnm, "w+"))
    users = User.select(
        *[User.id, User.language, User.update_date.alias("updated_date"), User.update_time,
          User.create_date.alias("created_time"),
          User.login_channel.alias("signup_application"), User.avatar, User.nickname.alias("display_name"),
          User.password,
          User.color_schema.alias("tag"), User.timezone.alias("region"), User.email]). \
        where(
        [User.update_time >= from_dt]
    ).order_by(User.update_time)
    tms = []
    for u in tqdm(list(users)):
        tms.append(str(u.update_time) + "\n")
        u = u.to_dict()
        flds = [k for k in u.keys() if u[k] is None]
        for k in flds:
            del u[k]
        u["owner"] = "infiniflow"
        u["name"] = u["email"]
        if "updated_date" in u:
            u["updated_time"] = u["updated_date"]
            del u["updated_date"]
        else:
            u["updated_time"] = datetime.fromtimestamp(u["update_time"] / 1000.).strftime("%Y-%m-%d %H:%M:%S")
            del u["update_time"]
        if u.get("password", "").find("scrypt:") >= 0:
            del u["password"]
        if "password" in u and not u["password"]:
            del u["password"]

        cu = CasdoorUser(**u)
        cus = CasdoorUser.select().where(CasdoorUser.email == u["email"])
        if len(cus) > 0:
            CasdoorUser.update(u).where(CasdoorUser.id == u["id"]).execute()
        else:
            cu.save(force_insert=True)

        if len(tms) % 10 == 9:
            f.writelines(tms)
            tms = []
    if tms:
        f.writelines(tms)
    f.close()


def rm_dup_user():
    cus = CasdoorUser.select().where(CasdoorUser.password.is_null(False))
    for cu in tqdm(list(cus)):
        cu_without_pswd = list(CasdoorUser.select().where(
            CasdoorUser.email == cu.email,
            CasdoorUser.password.is_null(True)
        ))
        for u in cu_without_pswd:
            CasdoorUser.delete().where(CasdoorUser.id == u.id).execute()


if __name__ == "__main__":
    while True:
        try:
            sync2casdoor(sys.argv[1])
        except Exception as e:
            print("【EXCEPTION】: ", e)
        time.sleep(1)
