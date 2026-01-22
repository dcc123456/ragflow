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
    permissions: Record<string, PermissionData>;
  };

  export type RoleDetailWithPermission = {
    role: {
      id: string;
      name: string;
      description: string;
    };
    permissions: Record<string, PermissionData>;
  };

  export type RoleDetail = {
    id: string;
    name: string;
    description: string;
    create_date: string;
    update_date: string;
  };

  export type AssignRolePermissionsInput = Record<
    string,
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
    resource_types: string[];
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
    type TypecastMap = {
      string: string;
      bool: boolean;
      integer: number;
    };

    type VariableDataRaw<
      N extends string = string,
      DT extends keyof TypecastMap = keyof TypecastMap,
      S extends string = 'variable',
    > = {
      source: S;
      data_type: DT;
      name: N;
      value: string;
    };

    type GetDataRaw<T extends object, S extends string = 'variable'> = {
      [K in keyof T]: VariableDataRaw<K, T[K], S>;
    };

    type RetypeByTypeAnnotation<
      D extends Record<
        string,
        VariableDataRaw<string, keyof TypecastMap, unknown>
      >,
      M extends TypecastMap = TypecastMap,
    > = {
      [K in keyof D]: Omit<D[K], 'value'> & {
        value: M[D[K]['data_type']] extends number
          ? number | null
          : M[D[K]['data_type']];
      };
    };

    type NameSeparator = '.';

    export namespace Common {
      type MailFieldNamePrefix = 'mail';

      type BasicFieldNames = 'enable_whitelist' | 'default_role';
      type MailFieldNames =
        | 'server'
        | 'port'
        | 'timeout'
        | 'username'
        | 'password'
        | 'default_sender'
        | 'use_ssl'
        | 'use_tls';

      type Basic = GetDataRaw<{
        enable_whitelist: 'bool';
        default_role: 'string';
      }>;

      type Mail = GetDataRaw<
        PrefixKeys<
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
          MailFieldNamePrefix
        >
      >;

      type All = Basic & Mail;
    }

    export namespace SSO {
      export namespace IDP {
        type ProviderId = 'google' | 'github' | 'feishu';
      }

      export namespace LDAP {
        type ServerId = string;
      }

      type GoogleFieldNamePrefix = 'google|sso';
      type GitHubFieldNamePrefix = 'github|sso';
      type FeishuFieldNamePrefix = 'feishu|sso';
      type LDAPFieldNamePrefix = `ldap|${string}`;

      type GoogleFieldNames =
        | 'enabled'
        | 'client_id'
        | 'client_secret'
        | 'redirect_uri';
      type GitHubFieldNames = 'enabled' | 'client_id' | 'secret_key' | 'url';
      type FeishuFieldNames =
        | 'enabled'
        | 'app_id'
        | 'app_secret'
        | 'app_access_token_url'
        | 'user_access_token_url';
      type LDAPFieldNames =
        | 'enabled'
        | 'name'
        | 'url'
        | 'dn'
        | 'password'
        | 'search_filter'
        | 'attribute_list';

      type Google = GetDataRaw<
        PrefixKeys<
          {
            enabled: 'bool';
            client_id: 'string';
            client_secret: 'string';
            redirect_uri: 'string';
          },
          GoogleFieldNamePrefix
        >,
        GoogleFieldNamePrefix
      >;

      type GitHub = GetDataRaw<
        PrefixKeys<
          {
            enabled: 'bool';
            client_id: 'string';
            secret_key: 'string';
            url: 'string';
          },
          GitHubFieldNamePrefix
        >,
        GitHubFieldNamePrefix
      >;

      type Feishu = GetDataRaw<
        PrefixKeys<
          {
            enabled: 'bool';
            app_id: 'string';
            app_secret: 'string';
            app_access_token_url: 'string';
            user_access_token_url: 'string';
          },
          FeishuFieldNamePrefix
        >,
        FeishuFieldNamePrefix
      >;

      type LDAP = GetDataRaw<
        PrefixKeys<
          {
            enabled: 'bool';
            name: 'string';
            url: 'string';
            dn: 'string';
            password: 'string';
            search_filter: 'string';
            attribute_list: 'string';
          },
          LDAPFieldNamePrefix
        >,
        LDAPFieldNamePrefix
      >;

      type AllFieldNamePrefix =
        | GoogleFieldNamePrefix
        | GitHubFieldNamePrefix
        | FeishuFieldNamePrefix
        | LDAPFieldNamePrefix;
      type All = Google & GitHub & Feishu & LDAP;
      type AllGrouped = {
        google: Google;
        github: GitHub;
        feishu: Feishu;
        ldap: LDAP;
      };
    }

    type All = Common.All & SSO.All;

    type VariableName = keyof All;

    type VariableDictionaryRaw = GetDataRaw<SSO.All, unknown>;
    type VariableDictionary = RetypeByTypeAnnotation<SSO.All>;
    type VariableRaw = ValueOf<VariableDictionaryRaw>;
    type Variable = ValueOf<VariableDictionary>;
  }

  export type SetVariablesInput = {
    [N in keyof SystemVariables.VariableDictionary]?: NonNullable<
      SystemVariables.VariableDictionary[N]['value']
    >;
  } & { [x: string]: any };

  export type DeleteVariablesInput = {
    source?: string;
    names?: keyof SystemVariables.VariableDictionary[];
  };

  export type SSOIDPSettings = {
    google: SystemVariables.RetypeByTypeAnnotation<SystemVariables.GoogleSSOVariablesValueTypeMap>;
    github: SystemVariables.RetypeByTypeAnnotation<SystemVariables.GitHubSSOVariablesValueTypeMap>;
    feishu: SystemVariables.RetypeByTypeAnnotation<SystemVariables.FeishuSSOVariablesValueTypeMap>;
  };

  export type LDAPSettings = {
    [
      x: string
    ]: SystemVariables.RetypeByTypeAnnotation<SystemVariables.LDAPVariablesValueTypeMap>;
  };
}
