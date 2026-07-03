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

from .oauth import OAuthClient, UserInfo


class FeishuOAuthClient(OAuthClient):
    def __init__(self, config):
        """
        Initialize the FeishuOAuthClient with the provider's configuration.
        """
        conf = {
            "authorization_url": "https://accounts.feishu.cn/open-apis/authen/v1/authorize",
            "token_url": "https://open.feishu.cn/open-apis/authen/v2/oauth/token",
            "userinfo_url": "https://open.feishu.cn/open-apis/authen/v1/user_info",
            "client_id": config["app_id"],
            "client_secret": config["app_secret"],
            "redirect_uri": "",
            "scope": "user_info",
        }
        super().__init__(conf)

    def normalize_user_info(self, user_info):
        email = user_info.get("email")
        username = user_info.get("name")
        nickname = user_info.get("en_name", username)
        avatar_url = user_info.get("avatar_url", "")
        return UserInfo(email=email, username=username, nickname=nickname, avatar_url=avatar_url)
