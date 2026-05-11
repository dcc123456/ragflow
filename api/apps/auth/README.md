# Auth

The Auth module provides implementations of OAuth2 and OpenID Connect (OIDC) authentication for integration with third-party identity providers.

**Features**

- Supports both OAuth2 and OIDC authentication protocols
- Automatic OIDC configuration discovery (via `/.well-known/openid-configuration`)
- JWT token validation
- Unified user information handling

## Configuration

### Database Storage

All OAuth/OIDC configurations are stored in the `SystemSettings` database table with the following source patterns:

| Provider | Source Pattern | Example Key |
|----------|--------------|-------------|
| GitHub | `<provider>\|sso` | `github\|sso.client_id` |
| Google | `<provider>\|sso` | `google\|sso.client_id` |
| LDAP | `ldap\|<id>` | `ldap\|default.enabled` |

### Configuration via Admin UI

Configure OAuth providers through the RAGFlow admin panel at: `Admin > SSO Providers`

### Manual Configuration Keys

If configuring directly in the database, use the following structure:

```python
# GitHub OAuth configuration (stored in SystemSettings table)
# Source: github|sso
github_config = {
    "type": "github",           # Required: identifies the provider type
    "client_id": "your_client_id",
    "client_secret": "your_client_secret",
}

# Google OAuth configuration
# Source: google|sso
google_config = {
    "type": "google",
    "client_id": "your_client_id",
    "client_secret": "your_client_secret",
    "authorization_url": "https://your-oauth-provider.com/oauth/authorize",
    "token_url": "https://your-oauth-provider.com/oauth/token",
    "userinfo_url": "https://your-oauth-provider.com/oauth/userinfo",
    "redirect_uri": "https://your-app.com/api/v1/auth/oauth/<channel>/callback"
}

# OIDC OAuth configuration
# Source: oidc|sso
oidc_config = {
    "type": "oidc",
    "issuer": "https://your-idp.com",  # OIDC issuer URL (e.g., Okta, Auth0)
    "client_id": "your_client_id",
    "client_secret": "your_client_secret",
    "redirect_uri": "https://your-app.com/api/v1/auth/oauth/<channel>/callback"
}

# Generic OAuth2 configuration
# Source: oauth2|sso
oauth2_config = {
    "type": "oauth2",
    "client_id": "your_client_id",
    "client_secret": "your_client_secret",
    "authorization_url": "https://your-oauth-provider.com/oauth/authorize",
    "token_url": "https://your-oauth-provider.com/oauth/token",
    "userinfo_url": "https://your-oauth-provider.com/oauth/userinfo",
    "scope": "openid email profile"
}
```

### Important Notes

1. **Redirect URI**: For GitHub and Google OAuth, the `redirect_uri` is configured in the provider's developer console (GitHub: Settings > OAuth Apps, Google: Google Cloud Console > Credentials). The RAGFlow callback endpoints are:
   - GitHub: `https://your-domain.com/v1/user/oauth/callback/github`
   - Google: `https://your-domain.com/v1/user/oauth/callback/google`

2. **Hardcoded Values**: The following values are automatically configured by the client library and do not need to be specified:
   - GitHub: authorization_url, token_url, userinfo_url, scope ("user:email")
   - Google: authorization_url, token_url, userinfo_url, scope ("openid email profile")

3. **LDAP**: LDAP configuration uses a different pattern with `ldap|<id>` as the source (e.g., `ldap|default`, `ldap|server-1`).

## Usage

```python
from api.apps.auth import get_auth_client

# Get OAuth client from database configuration
channel_config = SystemSettingsService.get_channel_oauth_config("github")
client = get_auth_client(channel_config)
```

### Authentication Flow

1. **Redirect to Authorization URL**:
```python
auth_url = client.get_authorization_url(state)
return redirect(auth_url)
```

2. **Exchange Authorization Code for Token** (after user authorizes):
```python
token_response = await client.async_exchange_code_for_token(authorization_code)
access_token = token_response["access_token"]
```

3. **Fetch User Information**:
```python
user_info = await client.async_fetch_user_info(access_token)
# Returns: UserInfo(email, username, nickname, avatar_url)
```

## User Information Structure

All authentication methods return user information following this structure:

```python
{
    "email": "user@example.com",
    "username": "username",
    "nickname": "User Name",
    "avatar_url": "https://example.com/avatar.jpg"
}
```

## Provider-Specific Notes

### GitHub

- Requires `client_id` and `client_secret` from [GitHub Developer Settings](https://github.com/settings/developers)
- Scope `user:email` is automatically added to retrieve user email
- Callback URL must be registered in GitHub OAuth App settings

### Google

- Requires `client_id` and `client_secret` from [Google Cloud Console](https://console.cloud.google.com/apis/credentials)
- Uses OpenID Connect protocol
- Callback URL must be registered in Google OAuth consent screen

### LDAP

- LDAP authentication uses username/password flow (not OAuth)
- Configuration includes: url, dn, password, search_filter, etc.
