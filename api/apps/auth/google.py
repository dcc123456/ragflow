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

from common.http_client import async_request, sync_request
from .oauth import OAuthClient, UserInfo


class GoogleOAuthClient(OAuthClient):
    """
    Google OAuth client.

    Uses the Google OpenID Connect compatible endpoints while preserving the
    generic OAuthClient interface (sync + async).
    """

    def __init__(self, config):
        config.update(
            {
                "authorization_url": "https://accounts.google.com/o/oauth2/v2/auth",
                "token_url": "https://oauth2.googleapis.com/token",
                "userinfo_url": "https://openidconnect.googleapis.com/v1/userinfo",
                # Google expects a space-delimited scope string.
                "scope": "openid email profile",
            }
        )
        super().__init__(config)

    def get_authorization_url(self, state=None):
        """
        Extend the base URL generation with Google specific hints.
        """
        base_url = super().get_authorization_url(state)
        # Ask Google to always re-check consent and issue refresh tokens when possible.
        joiner = "&" if "?" in base_url else "?"
        return f"{base_url}{joiner}access_type=offline&prompt=consent"

    def fetch_user_info(self, access_token, **kwargs):
        """
        Fetch Google user info (synchronous).
        """
        headers = {"Authorization": f"Bearer {access_token}"}
        try:
            response = sync_request("GET", self.userinfo_url, headers=headers, timeout=self.http_request_timeout)
            response.raise_for_status()
            return self.normalize_user_info(response.json())
        except Exception as e:
            raise ValueError(f"Failed to fetch google user info: {e}")

    async def async_fetch_user_info(self, access_token, **kwargs):
        """
        Async variant of fetch_user_info using httpx.
        """
        headers = {"Authorization": f"Bearer {access_token}"}
        try:
            response = await async_request(
                "GET",
                self.userinfo_url,
                headers=headers,
                timeout=self.http_request_timeout,
            )
            response.raise_for_status()
            return self.normalize_user_info(response.json())
        except Exception as e:
            raise ValueError(f"Failed to fetch google user info: {e}")

    def normalize_user_info(self, user_info):
        email = user_info.get("email")
        # Prefer the full name, then given_name, then the mailbox prefix.
        username = user_info.get("name") or user_info.get("given_name") or str(email).split("@")[0]
        nickname = user_info.get("name") or username
        avatar_url = user_info.get("picture", "")
        return UserInfo(email=email, username=username, nickname=nickname, avatar_url=avatar_url)
