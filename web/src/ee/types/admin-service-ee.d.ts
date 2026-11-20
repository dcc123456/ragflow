/*
 *  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
 *
 *  Licensed under the Apache License, Version 2.0 (the "License");
 *  you may not use this file except in compliance with the License.
 *  You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 *  Unless required by applicable law or agreed to in writing, software
 *  distributed under the License is distributed on an "AS IS" BASIS,
 *  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 *  See the License for the specific language governing permissions and
 *  limitations under the License.
 */

declare namespace AdminServiceEe {
  export type LoginData = {
    access_token: string;
    avatar: unknown;
    color_schema: 'Bright' | 'Dark';
    create_date: string;
    create_time: number;
    email: string;
    id: string;
    is_active: '0' | '1';
    is_anonymous: '0' | '1';
    is_authenticated: '0' | '1';
    is_superuser: boolean;
    language: string;
    last_login_time: string;
    login_channel: unknown;
    nickname: string;
    password: string;
    status: '0' | '1';
    timezone: string;
    update_date: [string];
    update_time: [number];
  };

  export type ListUsersItem = {
    create_date: number;
    email: string;
    is_active: '0' | '1';
    is_superuser: boolean;
    nickname: string;
    ldap_server?: string;
    last_login_time?: string;
    plan?: string;
  };

  export type UserDetail = {
    avatar?: string;
    create_date: string;
    email: string;
    is_active: '0' | '1';
    is_anonymous: '0' | '1';
    is_superuser: boolean;
    language: string;
    last_login_time: string;
    login_channel: unknown;
    status: '0' | '1';
    update_date: string;
    role: string;
  };

  export type CanvasCategory = 'agent_canvas' | 'dataflow_canvas';

  export type ListUserAssetsParams = {
    page?: number;
    page_size?: number;
    order_by?: 'create_time' | 'update_time';
    desc?: boolean;
  };

  export type ListUserAgentsParams = ListUserAssetsParams & {
    canvas_category: CanvasCategory;
  };

  export type ListUserDatasetItem = {
    id: string;
    avatar?: string | null;
    chunk_num: number;
    create_date: string;
    doc_num: number;
    file_size: number;
    language: string;
    name: string;
    status: '0' | '1';
    tenant_id: string;
    tenant_name: string;
    token_num: number;
    update_date: string;
  };

  export type ListUserAgentItem = {
    id: string;
    avatar?: string | null;
    canvas_category: CanvasCategory;
    title: string;
    tenant_id: string;
    tenant_name: string;
    create_date?: string;
    update_date?: string;
  };

  export type TaskExecutorHeartbeatItem = {
    name: string;
    boot_at: string;
    now: string;
    ip_address: string;
    current: Record<string, object>;
    done: number;
    failed: number;
    lag: number;
    pending: number;
    pid: number;
  };

  export type TaskExecutorInfo = Record<string, TaskExecutorHeartbeatItem[]>;

  export type ListServicesItem = {
    extra: Record<string, unknown>;
    host: string;
    id: number | string;
    name: string;
    port: number | string;
    service_type: string;
    status: string;
  };

  export type ServiceDetail =
    | {
        service_name: string;
        status: string;
        message: string | Record<string, any> | Record<string, any>[];
      }
    | {
        service_name: 'task_executor';
        status: 'alive' | 'timeout';
        message: AdminServiceEe.TaskExecutorInfo;
      };

  export type PermissionType = 'enable' | 'read' | 'write' | 'share';

  export type RoleResourceName =
    | 'dataset'
    | 'chat'
    | 'agent'
    | 'search'
    | 'file'
    | 'team'
    | 'memory'
    | 'model_provider';

  export type PermissionData = {
    enable: boolean;
    read: boolean;
    write: boolean;
    share: boolean;
  };

  export type ListRoleItem = {
    id: number;
    role_name: string;
    description: string | null;
    create_date: string | null;
    update_date: string | null;
  };

  export type ListRoleItemWithPermission = ListRoleItem & {
    permissions: Record<RoleResourceName, PermissionData>;
  };

  export type RoleDetailWithPermission = {
    role: {
      id: number;
      role_name: string;
      description: string | null;
    };
    permissions: Record<RoleResourceName, PermissionData>;
  };

  export type RoleDetail = {
    id: number;
    role_name: string;
    description: string | null;
    create_date: string | null;
    update_date: string | null;
  };

  export type AssignRolePermissionsInput = Record<
    RoleResourceName,
    Partial<PermissionData>
  >;
  export type RevokeRolePermissionInput = AssignRolePermissionsInput;

  export type UserDetailWithPermission = {
    user: {
      id: string;
      username: string;
      role: string;
    };
    role_permissions: Record<string, PermissionData>;
  };

  export type ResourceType = {
    resource_types: RoleResourceName[];
  };

  export type ListWhitelistItem = {
    id: number;
    email: string;
    create_date: string;
    create_time: number;
    update_date: string;
    update_time: number;
  };

  export type TestSMTPConnectionInput = {
    host: string;
    port: number;
    username: string;
    password: string;
    use_ssl?: boolean;
    use_tls?: boolean;
    timeout?: number;
  };

  export namespace SystemVariables {
    namespace Utilities {
      type DefineVariables<
        Defs extends Record<string, Types.DataType>,
        NamePrefix extends string = '',
        Source extends Types.SourceName = 'variable',
      > = {
        [N in keyof Defs]: {
          source: Source;
          data_type: Defs[N];
          name: '' extends NamePrefix
            ? N
            : `${NamePrefix}${Types.NameSeparator}${N}`;
          value: Types.ValueDataTypeMap[Defs[N]];
        };
      };

      type ExtractValue<T extends Record<string, { value: DataType }>> = {
        [K in keyof T]: T[K]['value'];
      };
    }

    namespace Types {
      type NameSeparator = '.';
      type ValueDataTypeMap = {
        bool: boolean;
        integer: number | null; // null if the value is not a number
        string: string;
      };

      type SourceName =
        | 'variable'
        | 'google|sso'
        | 'github|sso'
        | 'feishu|sso'
        | 'wecom|sso'
        | 'dingtalk|sso'
        | `ldap|${string}`;
      type DataType = keyof ValueDataTypeMap;

      type BoolType<O = object> = O & { data_type: 'bool'; value: boolean };
      type IntegerType<O = object> = O & {
        data_type: 'integer';
        value?: number;
      };
      type StringType<O = object> = O & { data_type: 'string'; value: string };
    }

    type Basic = Utilities.DefineVariables<{
      enable_whitelist: 'bool';
      default_role: 'string';
    }>;

    type notification = Utilities.DefineVariables<{
      'notification.enabled': 'bool';
      'notification.content': 'string';
    }>;

    type ExposeModelProvider = Utilities.DefineVariables<{
      'expose_model_provider.enabled': 'bool';
    }>;

    type ExposeApi = Utilities.DefineVariables<{
      'expose_api.enabled': 'bool';
    }>;

    type EmailVerification = Utilities.DefineVariables<{
      'email_verification.enabled': 'bool';
    }>;

    type ApiKey = Utilities.DefineVariables<{
      'api_key.max_count': 'integer';
    }>;

    type SensitiveWordFilter = Utilities.DefineVariables<{
      'sensitive_word_filter.enabled': 'bool';
      'sensitive_word_filter.response': 'string';
      'sensitive_word_filter.file_version': 'string';
      'sensitive_word_filter.file_name': 'string';
    }>;

    type Mail = Utilities.DefineVariables<
      {
        server: 'string';
        port: 'integer';
        timeout: 'integer';
        username: 'string';
        password: 'string';
        default_sender: 'string';
        use_ssl: 'bool';
        use_tls: 'bool';
      },
      'mail'
    >;

    export namespace SSO {
      export namespace IDP {
        export type ProviderId =
          | 'google'
          | 'github'
          | 'feishu'
          | 'wecom'
          | 'dingtalk';
        export type Google = Utilities.DefineVariables<
          {
            enabled: 'bool';
            client_id: 'string';
            client_secret: 'string';
            redirect_uri: 'string';
          },
          'google|sso',
          'google|sso'
        >;

        export type GitHub = Utilities.DefineVariables<
          {
            enabled: 'bool';
            client_id: 'string';
            client_secret: 'string';
          },
          'github|sso',
          'github|sso'
        >;

        export type Feishu = Utilities.DefineVariables<
          {
            enabled: 'bool';
            client_id: 'string';
            client_secret: 'string';
            redirect_uri: 'string';
            scope: 'string';
          },
          'feishu|sso',
          'feishu|sso'
        >;

        export type Wecom = Utilities.DefineVariables<
          {
            enabled: 'bool';
            client_id: 'string';
            client_secret: 'string';
            agent_id: 'string';
            redirect_uri: 'string';
          },
          'wecom|sso',
          'wecom|sso'
        >;

        export type Dingtalk = Utilities.DefineVariables<
          {
            enabled: 'bool';
            client_id: 'string';
            client_secret: 'string';
            redirect_uri: 'string';
            scope: 'string';
          },
          'dingtalk|sso',
          'dingtalk|sso'
        >;
      }

      export type LDAP = Utilities.DefineVariables<
        {
          enabled: 'bool';
          name: 'string';
          url: 'string';
          dn: 'string';
          password: 'string';
          search_filter: 'string';
          attribute_list: 'string';
        },
        `ldap|${string}`,
        `ldap|${string}`
      >;
    }

    type Name = keyof SystemVariables;
  }

  type SystemVariables = SystemVariables.Basic &
    SystemVariables.notification &
    SystemVariables.ExposeModelProvider &
    SystemVariables.ExposeApi &
    SystemVariables.EmailVerification &
    SystemVariables.ApiKey &
    SystemVariables.SensitiveWordFilter &
    PrefixKeys<SystemVariables.Mail, 'mail'> &
    PrefixKeys<SystemVariables.SSO.IDP.Google, 'google|sso'> &
    PrefixKeys<SystemVariables.SSO.IDP.GitHub, 'github|sso'> &
    PrefixKeys<SystemVariables.SSO.IDP.Feishu, 'feishu|sso'> &
    PrefixKeys<SystemVariables.SSO.IDP.Wecom, 'wecom|sso'> &
    PrefixKeys<SystemVariables.SSO.IDP.Dingtalk, 'dingtalk|sso'> &
    PrefixKeys<SystemVariables.SSO.LDAP, `ldap|${string}`>;

  export type SetVariablesInput = Partial<
    SystemVariables.Utilities.ExtractValue<SystemVariables>
  >;

  export type DeleteVariablesInput = {
    source?: SystemVariables.Types.SourceName;
    names?: SystemVariables.Name[];
  };

  export type RefreshVariablesInput = {
    oauth?: boolean;
    smtp?: boolean;
  };

  export type RoleDefaultModelType =
    | 'llm'
    | 'embedding'
    | 'vlm'
    | 'asr'
    | 'rerank'
    | 'tts';
  export type RoleDefaultModelSetupStatus = 'complete' | 'partial' | 'not_set';

  export type RoleDefaultModelItem = {
    role_id: number;
    model_type: RoleDefaultModelType;
    model_id: string;
    tenant_id: string;
  };

  export type RoleDefaultModelList = {
    model_list: RoleDefaultModelItem[];
    setup_status: RoleDefaultModelSetupStatus;
  };

  export type SetRoleDefaultModelInput = {
    model_type: RoleDefaultModelType;
    model_id: string;
  };

  export type AddLlmFactoryInput = {
    llm_factory: string;
    [x: string]: any;
  };

  // Sandbox settings types
  export type SandboxProvider = {
    id: string;
    name: string;
    description: string;
    tags: string[];
  };

  export type SandboxConfigFieldBase = {
    required?: boolean;
    label?: string;
    placeholder?: string;
    description?: string;
    multiline?: boolean;
    readonly?: boolean;
    scope?: 'runtime' | 'deployment';
  };

  export type SandboxConfigStringField = SandboxConfigFieldBase & {
    type: 'string';
    default?: string;
    secret?: boolean;
  };

  export type SandboxConfigIntegerField = SandboxConfigFieldBase & {
    type: 'integer';
    default?: number;
    min?: number;
    max?: number;
  };

  export type SandboxConfigBooleanField = SandboxConfigFieldBase & {
    type: 'boolean';
    default?: boolean;
  };

  export type SandboxConfigJsonField = SandboxConfigFieldBase & {
    type: 'json';
    default?: unknown;
  };

  export type SandboxConfigField =
    | SandboxConfigStringField
    | SandboxConfigIntegerField
    | SandboxConfigBooleanField
    | SandboxConfigJsonField;

  export type SandboxConfig = {
    provider_type: string;
    config: Record<string, unknown>;
  };

  export type SandboxProviderSchema = {
    provider_type: string;
    name: string;
    description: string;
    fields: Record<string, SandboxConfigField>;
  };

  export type SetSandboxConfigInput = {
    provider_type: string;
    config: Record<string, unknown>;
  };

  export type TestSandboxConnectionInput = {
    provider_type: string;
    config: Record<string, unknown>;
  };

  export type SandboxTestResult = {
    success: boolean;
    message?: string;
    details?: Record<string, unknown>;
  };

  // Token usage statistics types
  export type TokenStatsGranularity = 'hour' | 'day' | 'month';

  export type TokenStatsRangeParams = {
    from: string;
    to: string;
  };

  export type TokenStatsDetailParams = TokenStatsRangeParams & {
    user_name: string;
    granularity: TokenStatsGranularity;
  };

  export type TokenStatsUsersParams = TokenStatsRangeParams & {
    top: number;
  };

  export type TokenStatsDetailItem = {
    stat_date: string;
    user_id: string;
    email: string;
    group_id: string;
    group_name: string;
    app_id: string;
    app_name: string;
    provider_name: string;
    model_name: string;
    instance_name?: string;
    request_count: number;
    total_input_tokens: number;
    total_output_tokens: number;
    total_tokens: number;
    avg_response_time_ms: number;
    error_count: number;
    success_rate: number;
  };

  export type TokenStatsUserItem = {
    user_id: string;
    email: string;
    request_count: number;
    total_input_tokens: number;
    total_output_tokens: number;
    total_tokens: number;
    avg_response_time_ms: number;
    error_count: number;
    success_rate: number;
  };

  export type TokenStatsSummary = TokenStatsRangeParams & {
    from_date: string;
    to_date: string;
    request_count: number;
    total_input_tokens: number;
    total_output_tokens: number;
    total_tokens: number;
    avg_response_time_ms: number;
    error_count: number;
    success_rate: number;
  };
  // License types
  export type LicenseType = 'trial' | 'standard' | 'perpetual' | 'saas';
  export type LicenseStatus = 'valid' | 'expired' | 'pending' | 'inactive';

  export type LicenseInfo = {
    ID: string;
    Version: string;
    Type: string;
    CustomerName: string;
    IssuedAt: string;
    ValidFrom: string;
    ValidUntil: string;
  };

  export type SetLicenseInput = {
    license: string;
  };

  export type FingerprintResponse = {
    fingerprint: string;
  };
}
