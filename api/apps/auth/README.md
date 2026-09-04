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
