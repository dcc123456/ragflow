declare namespace AdminService {
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
    create_date: string;
    email: string;
    is_active: '0' | '1';
    is_superuser: boolean;
    role: string;
    nickname: string;
    ldap_server?: string;
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

  export type ListUserDatasetItem = {
    avatar?: string;
    chunk_num: number;
    create_date: string;
    doc_num: number;
    language: string;
    name: string;
    permission: string;
    status: '0' | '1';
    token_num: number;
    update_date: string;
  };

  export type ListUserAgentItem = {
    avatar?: string;
    canvas_category: 'agent';
    permission: 'string';
    title: string;
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
    id: number;
    name: string;
    port: number;
    service_type: string;
    status: 'alive' | 'timeout' | 'fail';
  };

  export type ServiceDetail =
    | {
        service_name: string;
        status: 'alive' | 'timeout';
        message: string | Record<string, any> | Record<string, any>[];
      }
    | {
        service_name: 'task_executor';
        status: 'alive' | 'timeout';
        message: AdminService.TaskExecutorInfo;
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
    id: string;
    role_name: string;
    description: string;
    create_date: string;
    update_date: string;
  };

  export type ListRoleItemWithPermission = ListRoleItem & {
    permissions: Record<RoleResourceName, PermissionData>;
  };

  export type RoleDetailWithPermission = {
    role: {
      id: string;
      name: string;
      description: string;
    };
    permissions: Record<RoleResourceName, PermissionData>;
  };

  export type RoleDetail = {
    id: string;
    name: string;
    description: string;
    create_date: string;
    update_date: string;
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
        export type ProviderId = 'google' | 'github' | 'feishu';
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
            secret_key: 'string';
          },
          'github|sso',
          'github|sso'
        >;

        export type Feishu = Utilities.DefineVariables<
          {
            enabled: 'bool';
            app_id: 'string';
            app_secret: 'string';
            app_access_token_url: 'string';
            user_access_token_url: 'string';
          },
          'feishu|sso',
          'feishu|sso'
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
    PrefixKeys<SystemVariables.Mail, 'mail'> &
    PrefixKeys<SystemVariables.SSO.IDP.Google, 'google|sso'> &
    PrefixKeys<SystemVariables.SSO.IDP.GitHub, 'github|sso'> &
    PrefixKeys<SystemVariables.SSO.IDP.Feishu, 'feishu|sso'> &
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
}
