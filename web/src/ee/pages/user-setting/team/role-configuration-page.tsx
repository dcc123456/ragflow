import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import { Input } from '@/components/ui/input';
import { Switch } from '@/components/ui/switch';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import {
  DirectModelTreeSelect,
  ModelTreeSelect,
} from '@/components/model-tree-select';
import message from '@/components/ui/message';
import { LlmIcon } from '@/components/svg-icon';
import { useDataSourceInfo } from '@/pages/user-setting/data-source/constant';
import { getAvailableDataSourceKeys } from '@/pages/user-setting/data-source/backend-adapter';
import { eeDataSourcePermissionKeys } from '@/ee/pages/user-setting/data-source/ee-extensions';
import {
  ChatChannelKey,
  useChatChannelInfo,
} from '@/pages/user-setting/chat-channel/constant';
import { RolePermissionMatrix } from '@/ee/components/role-permission-matrix';
import { ModelReferenceSelect } from '@/ee/components/model-reference-select';
import {
  ITenantRolePermissionMatrix,
  TenantRoleDefaultModelType,
} from '@/ee/interfaces/database/team';
import { useFetchAvailableProviders } from '@/hooks/use-llm-request';
import {
  useFetchTenantInfo,
  useFetchUserInfo,
} from '@/hooks/use-user-setting-request';
import { Routes } from '@/routes';
import {
  ArrowLeft,
  ChevronDown,
  ChevronRight,
  Eye,
  EyeOff,
  LoaderCircle,
  Pencil,
  PencilOff,
  Search,
  ScreenShare,
  ScreenShareOff,
} from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useLocation, useNavigate, useParams } from 'react-router';
import { useTranslation } from 'react-i18next';
import '@/ee/services/admin-llm-scope';
import {
  getRolePermissions as getAdminRolePermissions,
  listResources as listAdminRoleResources,
  setRolePermissionMatrix,
  getRoleDefaultModels as getAdminRoleDefaultModels,
  setRoleDefaultModel as setAdminRoleDefaultModel,
} from '@/ee/services/admin-service';
import {
  useTenantMyPermissions,
  useTenantRoles,
} from './use-member-role-query';
import {
  useSetTenantRoleDefaultModel,
  useSetTenantRolePermissions,
  useTenantRoleDefaultModels,
  useTenantRolePermissions,
  useTenantRoleResources,
} from './use-tenant-role-management';

const DEFAULT_MODEL_OPTIONS: {
  type: TenantRoleDefaultModelType;
  modelTypes: string[];
}[] = [
  { type: 'chat', modelTypes: ['chat'] },
  { type: 'embedding', modelTypes: ['embedding'] },
  { type: 'rerank', modelTypes: ['rerank'] },
  { type: 'asr', modelTypes: ['asr'] },
  { type: 'vision', modelTypes: ['vision'] },
  { type: 'tts', modelTypes: ['tts'] },
  { type: 'ocr', modelTypes: ['ocr'] },
  { type: 'decision', modelTypes: ['decision'] },
  { type: 'scoring', modelTypes: ['scoring'] },
];
const DISPLAY_ACTION_ORDER = ['read', 'write', 'share', 'enable'];
const ASSET_RESOURCE_ORDER = [
  'dataset',
  'memory',
  'chat',
  'search',
  'file',
  'agent',
];
const INTEGRATION_RESOURCE_ORDER = [
  'model_provider',
  'model_group',
  'data_source',
  'chat_channel',
  'mcp',
];
const INTEGRATION_RESOURCE_TYPES = new Set(INTEGRATION_RESOURCE_ORDER);
const ORGANIZATION_RESOURCE_ORDER = ['group', 'invitation'];
const ORGANIZATION_RESOURCE_TYPES = new Set(ORGANIZATION_RESOURCE_ORDER);
const PROVIDER_PERMISSION_PREFIX = 'model_provider::';
const DATA_SOURCE_PERMISSION_PREFIX = 'data_source::';
const CHAT_CHANNEL_PERMISSION_PREFIX = 'chat_channel::';
const MCP_ACTION_ICONS = {
  read: { enabled: Eye, disabled: EyeOff },
  write: { enabled: Pencil, disabled: PencilOff },
  share: { enabled: ScreenShare, disabled: ScreenShareOff },
} as const;
const SUPPORTED_CHAT_CHANNELS = [
  ChatChannelKey.DISCORD,
  ChatChannelKey.DINGTALK,
  ChatChannelKey.FEISHU,
  ChatChannelKey.TELEGRAM,
  ChatChannelKey.QQBOT,
  ChatChannelKey.WECOM,
  ChatChannelKey.WHATSAPP,
];

const providerPermissionKey = (providerName: string) =>
  `${PROVIDER_PERMISSION_PREFIX}${providerName}`;
const dataSourcePermissionKey = (source: string) =>
  `${DATA_SOURCE_PERMISSION_PREFIX}${source}`;
const chatChannelPermissionKey = (channel: string) =>
  `${CHAT_CHANNEL_PERMISSION_PREFIX}${channel}`;

function IntegrationItemPicker({
  items,
  selected,
  enabled,
  onToggle,
}: {
  items: { id: string; name: string; icon?: React.ReactNode }[];
  selected: (id: string) => boolean;
  enabled: boolean;
  onToggle: (id: string, checked: boolean) => void;
}) {
  const { t } = useTranslation();
  const [search, setSearch] = useState('');
  const filteredItems = useMemo(() => {
    const keyword = search.trim().toLocaleLowerCase();
    return keyword
      ? items.filter((item) => item.name.toLocaleLowerCase().includes(keyword))
      : items;
  }, [items, search]);
  const selectedCount = filteredItems.filter((item) =>
    selected(item.id),
  ).length;
  const allSelected =
    filteredItems.length > 0 && selectedCount === filteredItems.length;
  const someSelected = selectedCount > 0 && !allSelected;

  return (
    <div className="mt-4 border-t border-border-button pt-4">
      <div className="mb-3 flex flex-wrap items-center gap-3">
        <label className="flex items-center gap-2 text-sm text-text-secondary">
          <Checkbox
            checked={someSelected ? 'indeterminate' : allSelected}
            disabled={!enabled || filteredItems.length === 0}
            onCheckedChange={(value) => {
              filteredItems.forEach((item) =>
                onToggle(item.id, value === true),
              );
            }}
          />
          {t('common.selectAll')}
        </label>
        <div className="relative w-full max-w-xs">
          <Search className="absolute left-3 top-1/2 size-4 -translate-y-1/2 text-text-secondary" />
          <Input
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder={t('common.search')}
            className="pl-9"
          />
        </div>
      </div>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-5">
        {filteredItems.map((item) => (
          <label
            key={item.id}
            className="flex min-w-0 items-center gap-3 rounded-xl bg-bg-card px-4 py-3 text-sm text-text-primary transition-colors hover:bg-bg-input"
          >
            <Checkbox
              checked={selected(item.id)}
              disabled={!enabled}
              aria-label={item.name}
              onCheckedChange={(value) => onToggle(item.id, value === true)}
            />
            {item.icon && (
              <span className="flex size-8 shrink-0 items-center justify-center [&_svg]:max-h-8 [&_svg]:max-w-8">
                {item.icon}
              </span>
            )}
            <span className="min-w-0 truncate">{item.name}</span>
          </label>
        ))}
      </div>
    </div>
  );
}

const sortResources = <T extends { resource_type: string }>(
  resources: T[],
  order: string[],
) => {
  const orderIndex = new Map(order.map((resource, index) => [resource, index]));
  return [...resources].sort(
    (left, right) =>
      (orderIndex.get(left.resource_type) ?? order.length) -
      (orderIndex.get(right.resource_type) ?? order.length),
  );
};

export default function TenantRoleConfigurationPage({
  adminMode = false,
}: {
  adminMode?: boolean;
}) {
  const { roleId: roleIdParam, roleName } = useParams();
  const tenantRoleId = Number(roleIdParam);
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const location = useLocation();
  const { t } = useTranslation();
  const defaultModelLabels: Record<TenantRoleDefaultModelType, string> = {
    chat: t('setting.chatModel'),
    embedding: t('setting.embeddingModel'),
    rerank: t('setting.rerankModel'),
    asr: t('setting.sequence2txtModel'),
    vision: t('setting.img2txtModel'),
    tts: t('setting.ttsModel'),
    ocr: 'OCR',
    decision: t('setting.decisionModel'),
    scoring: t('setting.scoringModel'),
  };
  const { data: tenantInfo, loading: loadingTenant } =
    useFetchTenantInfo(!adminMode);
  const { data: userInfo, loading: loadingUser } = useFetchUserInfo(!adminMode);
  const { data: availableProviders } = useFetchAvailableProviders();
  const { dataSourceInfo } = useDataSourceInfo();
  const { chatChannelInfo } = useChatChannelInfo();
  const tenantId = tenantInfo.tenant_id;
  const adminRoleQuery = useQuery({
    queryKey: ['admin/roleConfiguration', roleName],
    enabled: adminMode && !!roleName,
    queryFn: async () => {
      const response = await getAdminRolePermissions(roleName!);
      if (response.data.code !== 0) throw new Error(response.data.message);
      return response.data.data;
    },
  });
  const adminResourceQuery = useQuery({
    queryKey: ['admin/roleResources'],
    enabled: adminMode,
    queryFn: async () => {
      const response = await listAdminRoleResources();
      if (response.data.code !== 0) throw new Error(response.data.message);
      return response.data.data.resource_types.map((resource_type) => ({
        resource_type,
        actions: [
          'invitation',
          'model_provider',
          'model_group',
          'data_source',
          'chat_channel',
        ].includes(resource_type)
          ? ['enable']
          : ['enable', 'read', 'write', 'share'],
      }));
    },
    initialData: [],
  });
  const adminDefaultModelsQuery = useQuery({
    queryKey: ['admin/roleDefaultModels', roleName],
    enabled: adminMode && !!roleName,
    queryFn: async () => {
      const response = await getAdminRoleDefaultModels(roleName!);
      if (response.data.code !== 0) throw new Error(response.data.message);
      return response.data.data;
    },
  });
  const roleId = adminMode
    ? Number(adminRoleQuery.data?.role.id ?? 0)
    : tenantRoleId;
  const {
    data: myPermissions,
    loading: loadingMyPermissions,
    isError: myPermissionsError,
  } = useTenantMyPermissions(tenantId, !adminMode);
  const canManageRoles =
    adminMode || !!myPermissions?.tenant_permissions.manage_roles;
  const { data: roles } = useTenantRoles(
    tenantId,
    canManageRoles && !adminMode,
  );
  const role = adminMode
    ? adminRoleQuery.data?.role
    : roles.find((item) => item.id === roleId);
  const {
    data: tenantResources,
    loading: tenantLoadingResources,
    isError: tenantResourcesError,
  } = useTenantRoleResources(tenantId, canManageRoles && !adminMode);
  const {
    data: tenantPermissionsResult,
    loading: tenantLoadingPermissions,
    isError: tenantPermissionsError,
  } = useTenantRolePermissions(tenantId, roleId, canManageRoles && !adminMode);
  const {
    data: tenantDefaultModelsResult,
    loading: loadingDefaultModels,
    isError: tenantDefaultModelsError,
  } = useTenantRoleDefaultModels(
    tenantId,
    roleId,
    canManageRoles && !adminMode,
  );
  const { loading: savingPermissions, setPermissions } =
    useSetTenantRolePermissions(tenantId, roleId);
  const { loading: savingDefaultModel, setDefaultModel } =
    useSetTenantRoleDefaultModel(tenantId, roleId);
  const resources = adminMode ? adminResourceQuery.data : tenantResources;
  const loadingResources = adminMode
    ? adminResourceQuery.isLoading
    : tenantLoadingResources;
  const resourcesError = adminMode
    ? adminResourceQuery.isError
    : tenantResourcesError;
  const permissionsResult = adminMode
    ? adminRoleQuery.data
    : tenantPermissionsResult;
  const loadingPermissions = adminMode
    ? adminRoleQuery.isLoading
    : tenantLoadingPermissions;
  const permissionsError = adminMode
    ? adminRoleQuery.isError
    : tenantPermissionsError;
  const defaultModelsResult = adminMode
    ? adminDefaultModelsQuery.data
    : tenantDefaultModelsResult;
  const defaultModelsError = adminMode
    ? adminDefaultModelsQuery.isError
    : tenantDefaultModelsError;
  const [permissions, setPermissionsDraft] =
    useState<ITenantRolePermissionMatrix>({});
  const [activeTab, setActiveTab] = useState('assets');
  const [defaultModelsExpanded, setDefaultModelsExpanded] = useState(true);
  const [modelProvidersExpanded, setModelProvidersExpanded] = useState(false);
  const [dataSourcesExpanded, setDataSourcesExpanded] = useState(false);
  const [chatChannelsExpanded, setChatChannelsExpanded] = useState(false);
  const [providerSearch, setProviderSearch] = useState('');
  const handleBack = () => {
    const state = location.state as {
      returnTo?: {
        pathname: string;
        search?: string;
        teamTab?: string;
        keyword?: string;
      };
    } | null;
    const returnTo = state?.returnTo;

    if (returnTo) {
      navigate(`${returnTo.pathname}${returnTo.search ?? ''}`, {
        state: {
          teamTab: returnTo.teamTab,
          teamKeyword: returnTo.keyword,
        },
      });
      return;
    }
    navigate(adminMode ? Routes.AdminRoles : Routes.Team);
  };

  useEffect(() => {
    if (permissionsResult?.permissions) {
      setPermissionsDraft(permissionsResult.permissions);
    }
  }, [permissionsResult?.permissions]);

  const assetResources = useMemo(
    () =>
      sortResources(
        resources.filter(
          (resource) =>
            !INTEGRATION_RESOURCE_TYPES.has(resource.resource_type) &&
            !ORGANIZATION_RESOURCE_TYPES.has(resource.resource_type),
        ),
        ASSET_RESOURCE_ORDER,
      ),
    [resources],
  );
  const providerNames = useMemo(
    () =>
      [...new Set(availableProviders.map((provider) => provider.name))].sort(
        (left, right) => left.localeCompare(right),
      ),
    [availableProviders],
  );
  const dataSourceItems = useMemo(
    () =>
      [
        ...new Set([
          ...getAvailableDataSourceKeys(),
          ...eeDataSourcePermissionKeys,
        ]),
      ].map((id) => ({
        id,
        name: dataSourceInfo[id as keyof typeof dataSourceInfo]?.name ?? id,
        icon: dataSourceInfo[id as keyof typeof dataSourceInfo]?.icon,
      })),
    [dataSourceInfo],
  );
  const chatChannelItems = useMemo(
    () =>
      SUPPORTED_CHAT_CHANNELS.map((id) => ({
        id,
        name: chatChannelInfo[id]?.name ?? id,
        icon: chatChannelInfo[id]?.icon,
      })),
    [chatChannelInfo],
  );
  const integrationResources = useMemo(() => {
    return sortResources(
      resources.filter((resource) =>
        INTEGRATION_RESOURCE_TYPES.has(resource.resource_type),
      ),
      INTEGRATION_RESOURCE_ORDER,
    );
  }, [resources]);
  const displayPermissions = useMemo(() => {
    const result = { ...permissions };
    const providersEnabled = permissions.model_provider?.enable === true;
    providerNames.forEach((providerName) => {
      const key = providerPermissionKey(providerName);
      result[key] = {
        enable: providersEnabled && (permissions[key]?.enable ?? true),
      };
    });
    dataSourceItems.forEach(({ id }) => {
      const key = dataSourcePermissionKey(id);
      result[key] = {
        enable:
          permissions.data_source?.enable === true &&
          (permissions[key]?.enable ?? true),
      };
    });
    chatChannelItems.forEach(({ id }) => {
      const key = chatChannelPermissionKey(id);
      result[key] = {
        enable:
          permissions.chat_channel?.enable === true &&
          (permissions[key]?.enable ?? true),
      };
    });
    return result;
  }, [permissions, providerNames, dataSourceItems, chatChannelItems]);
  const filteredProviderNames = useMemo(() => {
    const keyword = providerSearch.trim().toLocaleLowerCase();
    return keyword
      ? providerNames.filter((name) =>
          name.toLocaleLowerCase().includes(keyword),
        )
      : providerNames;
  }, [providerNames, providerSearch]);
  const selectedProviderCount = filteredProviderNames.filter(
    (name) => displayPermissions[providerPermissionKey(name)]?.enable,
  ).length;
  const allProvidersSelected =
    filteredProviderNames.length > 0 &&
    selectedProviderCount === filteredProviderNames.length;
  const someProvidersSelected =
    selectedProviderCount > 0 && !allProvidersSelected;
  const organizationResources = useMemo(
    () =>
      sortResources(
        resources.filter((resource) =>
          ORGANIZATION_RESOURCE_ORDER.includes(resource.resource_type),
        ),
        ORGANIZATION_RESOURCE_ORDER,
      ),
    [resources],
  );
  const handleToggle = (
    resourceName: string,
    action: string,
    checked: boolean,
    resourceActions: string[],
  ) => {
    setPermissionsDraft((current) => {
      const nextResourcePermissions = {
        ...current[resourceName],
        [action]: checked,
      };

      if (action === 'enable' && !checked) {
        resourceActions.forEach((dependentAction) => {
          nextResourcePermissions[dependentAction] = false;
        });
      }
      if (action === 'read' && !checked) {
        ['write', 'share'].forEach((dependentAction) => {
          if (resourceActions.includes(dependentAction)) {
            nextResourcePermissions[dependentAction] = false;
          }
        });
      }
      if (action === 'read' && checked && resourceActions.includes('enable')) {
        nextResourcePermissions.enable = true;
      }
      if (['write', 'share'].includes(action) && checked) {
        if (resourceActions.includes('enable')) {
          nextResourcePermissions.enable = true;
        }
        if (resourceActions.includes('read')) {
          nextResourcePermissions.read = true;
        }
      }

      const next = { ...current, [resourceName]: nextResourcePermissions };
      if (
        resourceName === 'model_provider' &&
        action === 'enable' &&
        !checked
      ) {
        providerNames.forEach((providerName) => {
          next[providerPermissionKey(providerName)] = { enable: false };
        });
      }
      if (resourceName === 'data_source' && action === 'enable' && !checked) {
        dataSourceItems.forEach(({ id }) => {
          next[dataSourcePermissionKey(id)] = { enable: false };
        });
      }
      if (resourceName === 'chat_channel' && action === 'enable' && !checked) {
        chatChannelItems.forEach(({ id }) => {
          next[chatChannelPermissionKey(id)] = { enable: false };
        });
      }
      return next;
    });
  };

  const handleSavePermissions = async () => {
    const normalizedPermissions = Object.fromEntries(
      Object.entries(permissions).map(([resourceName, resourcePermissions]) => {
        const resource = resources.find(
          (item) => item.resource_type === resourceName,
        );
        const normalized = { ...resourcePermissions };
        if (normalized.write || normalized.share) {
          if (resource?.actions.includes('enable')) normalized.enable = true;
          if (resource?.actions.includes('read')) normalized.read = true;
        } else if (normalized.read && resource?.actions.includes('enable')) {
          normalized.enable = true;
        }
        return [resourceName, normalized];
      }),
    );
    let code: number | undefined;
    if (adminMode && roleName) {
      const response = await setRolePermissionMatrix(
        roleName,
        normalizedPermissions,
      );
      if (response.data.code !== 0) throw new Error(response.data.message);
      await queryClient.invalidateQueries({
        queryKey: ['admin/roleConfiguration', roleName],
      });
      code = 0;
    } else {
      code = await setPermissions(normalizedPermissions);
    }

    if (code === 0) {
      message.success(t('permission.rolePermissionsUpdated'));
    }
  };

  const handleDefaultModelChange = async (
    modelType: TenantRoleDefaultModelType,
    modelId: string,
  ) => {
    let code: number | undefined;
    if (adminMode && roleName) {
      const response = await setAdminRoleDefaultModel(roleName, {
        model_type: modelType as never,
        model_id: modelId,
      });
      code = response.data.code;
      await queryClient.invalidateQueries({
        queryKey: ['admin/roleDefaultModels', roleName],
      });
    } else {
      code = await setDefaultModel({
        model_type: modelType,
        model_id: modelId,
      });
    }

    if (code === 0) {
      message.success(t('permission.roleDefaultModelUpdated'));
    }
  };

  const loading = adminMode
    ? adminRoleQuery.isLoading || adminResourceQuery.isLoading
    : loadingTenant || loadingMyPermissions;

  if (loading) {
    return (
      <div className="flex h-48 items-center justify-center text-text-secondary">
        <LoaderCircle className="mr-2 size-4 animate-spin" />
        {t('common.loading')}
      </div>
    );
  }

  if (!adminMode && (myPermissionsError || !myPermissions)) {
    return (
      <div className="mx-auto max-w-5xl space-y-4">
        <Button variant="ghost" onClick={handleBack}>
          <ArrowLeft /> {t('common.back')}
        </Button>
        <p className="rounded-lg border border-border-button p-6 text-state-error">
          {t('permission.roleConfigurationLoadFailed')}
        </p>
      </div>
    );
  }

  if (!canManageRoles) {
    return (
      <div className="mx-auto max-w-5xl space-y-4">
        <Button variant="ghost" onClick={handleBack}>
          <ArrowLeft /> {t('common.back')}
        </Button>
        <p className="rounded-lg border border-border-button p-6 text-state-error">
          {t('permission.roleConfigurationDenied')}
        </p>
      </div>
    );
  }

  if (
    (adminMode &&
      (!roleName || adminRoleQuery.isError || !adminRoleQuery.data)) ||
    (!adminMode && (!Number.isInteger(roleId) || roleId <= 0))
  ) {
    return (
      <div className="mx-auto max-w-5xl space-y-4">
        <Button variant="ghost" onClick={handleBack}>
          <ArrowLeft /> {t('common.back')}
        </Button>
        <p className="rounded-lg border border-border-button p-6 text-text-secondary">
          {t('permission.roleConfigurationLoadFailed')}
        </p>
      </div>
    );
  }

  return (
    <section className="flex size-full min-h-0 w-full flex-col overflow-hidden rounded-2xl border border-border-default bg-bg-base px-6 pb-4 pt-6 shadow-none">
      <header className="mb-6 flex items-center gap-3">
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label={t('common.back')}
          title={t('common.back')}
          onClick={handleBack}
        >
          <ArrowLeft />
        </Button>
        <h1 className="text-xl font-semibold text-text-primary">
          “{role?.role_name ?? `#${roleId}`}” {t('permission.roleManagement')}
        </h1>
      </header>

      <Tabs
        value={activeTab}
        onValueChange={setActiveTab}
        className="flex min-h-0 flex-1 flex-col"
      >
        <TabsList className="mb-5 h-auto justify-start gap-2 self-start rounded-none bg-transparent p-0">
          <TabsTrigger
            value="assets"
            className="rounded-full border border-border-button px-4 py-2"
          >
            {t('permission.assets')}
          </TabsTrigger>
          <TabsTrigger
            value="integrations"
            className="rounded-full border border-border-button px-4 py-2"
          >
            {t('permission.integrations')}
          </TabsTrigger>
          <TabsTrigger
            value="organization"
            className="rounded-full border border-border-button px-4 py-2"
          >
            {t('permission.organization')}
          </TabsTrigger>
        </TabsList>

        <TabsContent value="assets" className="mt-0">
          {loadingResources || loadingPermissions ? (
            <p className="p-6 text-center text-sm text-text-secondary">
              {t('common.loading')}
            </p>
          ) : resourcesError || permissionsError || !permissionsResult ? (
            <p className="p-6 text-center text-sm text-state-error">
              {t('permission.rolePermissionsLoadFailed')}
            </p>
          ) : (
            <RolePermissionMatrix
              resources={assetResources}
              permissions={permissions}
              resourceHeader={t('permission.resource')}
              getResourceLabel={(resource) =>
                t(`admin.resourceType.${resource}`)
              }
              getActionLabel={(action) => t(`admin.permissionType.${action}`)}
              actionOrder={DISPLAY_ACTION_ORDER}
              iconActions
              onChange={(resource, action, checked) => {
                const resourceActions =
                  resources.find((item) => item.resource_type === resource)
                    ?.actions ?? [];
                handleToggle(resource, action, checked, resourceActions);
              }}
            />
          )}
        </TabsContent>

        <TabsContent value="organization" className="mt-0">
          {loadingResources || loadingPermissions ? (
            <p className="p-6 text-center text-sm text-text-secondary">
              {t('common.loading')}
            </p>
          ) : resourcesError || permissionsError || !permissionsResult ? (
            <p className="p-6 text-center text-sm text-state-error">
              {t('permission.rolePermissionsLoadFailed')}
            </p>
          ) : (
            <RolePermissionMatrix
              resources={organizationResources}
              permissions={permissions}
              resourceHeader={t('permission.resource')}
              getResourceLabel={(resource) =>
                t(`admin.resourceType.${resource}`)
              }
              getActionLabel={(action) => t(`admin.permissionType.${action}`)}
              actionOrder={DISPLAY_ACTION_ORDER}
              iconActions
              onChange={(resource, action, checked) => {
                const resourceActions =
                  resources.find((item) => item.resource_type === resource)
                    ?.actions ?? [];
                handleToggle(resource, action, checked, resourceActions);
              }}
            />
          )}
        </TabsContent>

        <TabsContent
          value="integrations"
          className="mt-0 min-h-0 flex-1 space-y-3 overflow-y-auto pr-2 scrollbar-auto"
        >
          <section className="rounded-xl border border-border-button bg-bg-base p-4">
            <button
              type="button"
              className="flex min-h-8 w-full items-center gap-3 text-left"
              aria-expanded={defaultModelsExpanded}
              onClick={() => setDefaultModelsExpanded((expanded) => !expanded)}
            >
              <span className="flex size-8 shrink-0 items-center justify-center text-text-secondary">
                {defaultModelsExpanded ? (
                  <ChevronDown className="size-4" />
                ) : (
                  <ChevronRight className="size-4" />
                )}
              </span>
              <span className="min-w-0 flex-1 truncate text-base font-medium text-text-primary">
                {t('permission.roleDefaultModels')}
              </span>
              <span aria-hidden="true" className="h-4 w-7 shrink-0" />
            </button>
            {defaultModelsExpanded && (
              <div className="mt-4 space-y-3">
                <p className="text-sm text-text-secondary">
                  {t('permission.roleDefaultModelsDescription')}
                </p>
                {loadingDefaultModels ? (
                  <p className="text-sm text-text-secondary">
                    {t('common.loading')}
                  </p>
                ) : defaultModelsError || !defaultModelsResult ? (
                  <p className="text-sm text-state-error">
                    {t('permission.roleDefaultModelsLoadFailed')}
                  </p>
                ) : (
                  <div className="grid gap-x-8 gap-y-3 md:grid-cols-2 xl:grid-cols-3">
                    {DEFAULT_MODEL_OPTIONS.map((item) => {
                      const modelId =
                        defaultModelsResult.model_list.find(
                          (model) => model.model_type === item.type,
                        )?.model_id ?? '';

                      return (
                        <div
                          key={item.type}
                          className="flex min-w-0 items-center gap-3"
                        >
                          <label className="w-28 shrink-0 truncate text-sm font-medium text-text-primary">
                            {defaultModelLabels[item.type]}
                          </label>
                          {adminMode ? (
                            <ModelTreeSelect
                              className="min-w-0 flex-1 overflow-hidden [&>span]:min-w-0 [&>span]:flex-1"
                              skipTenantInfo
                              modelTypes={item.modelTypes}
                              value={modelId}
                              allowClear
                              placeholder={t('setting.selectModelPlaceholder')}
                              onChange={(value) =>
                                void handleDefaultModelChange(
                                  item.type,
                                  value ?? '',
                                )
                              }
                            />
                          ) : (
                            <ModelReferenceSelect
                              directModelSelect={DirectModelTreeSelect}
                              allowModelGroups
                              modelTypes={item.modelTypes}
                              className="min-w-0 flex-1 overflow-hidden"
                              resourceTenantId={tenantId}
                              ownerTenantId={tenantId}
                              accessUserId={userInfo.id}
                              skipTenantInfo
                              value={modelId}
                              allowClear
                              placeholder={t('setting.selectModelPlaceholder')}
                              disabled={
                                savingDefaultModel ||
                                loadingUser ||
                                !userInfo.id
                              }
                              onChange={(value) =>
                                void handleDefaultModelChange(
                                  item.type,
                                  value ?? '',
                                )
                              }
                            />
                          )}
                        </div>
                      );
                    })}
                  </div>
                )}
              </div>
            )}
          </section>

          {loadingResources || loadingPermissions ? (
            <p className="p-6 text-center text-sm text-text-secondary">
              {t('common.loading')}
            </p>
          ) : resourcesError || permissionsError || !permissionsResult ? (
            <p className="p-6 text-center text-sm text-state-error">
              {t('permission.rolePermissionsLoadFailed')}
            </p>
          ) : (
            <div className="space-y-3">
              <div className="grid grid-cols-1 gap-3">
                {integrationResources.map((resource) => {
                  const resourceType = resource.resource_type;
                  const isProvider = resourceType === 'model_provider';
                  const isDataSource = resourceType === 'data_source';
                  const isChatChannel = resourceType === 'chat_channel';
                  const isExpandable =
                    isProvider || isDataSource || isChatChannel;
                  const enabled = permissions[resourceType]?.enable === true;
                  const expanded = isProvider
                    ? modelProvidersExpanded
                    : isDataSource
                      ? dataSourcesExpanded
                      : chatChannelsExpanded;
                  const setExpanded = isProvider
                    ? setModelProvidersExpanded
                    : isDataSource
                      ? setDataSourcesExpanded
                      : setChatChannelsExpanded;
                  return (
                    <section
                      key={resource.resource_type}
                      className="min-w-0 rounded-xl border border-border-button bg-bg-base p-4"
                    >
                      <div className="flex items-center gap-3">
                        {isExpandable && (
                          <Button
                            variant="ghost"
                            size="icon-sm"
                            aria-label={t(`admin.resourceType.${resourceType}`)}
                            aria-expanded={expanded}
                            onClick={() => setExpanded((value) => !value)}
                          >
                            {expanded ? <ChevronDown /> : <ChevronRight />}
                          </Button>
                        )}
                        {!isExpandable && (
                          <span
                            aria-hidden="true"
                            className="size-8 shrink-0"
                          />
                        )}
                        <span className="min-w-0 flex-1 truncate text-base font-medium text-text-primary">
                          {t(`admin.resourceType.${resource.resource_type}`)}
                        </span>
                        {resourceType === 'mcp' &&
                          (['read', 'write', 'share'] as const).map(
                            (action) => {
                              const checked =
                                permissions.mcp?.[action] === true;
                              const Icon =
                                MCP_ACTION_ICONS[action][
                                  checked ? 'enabled' : 'disabled'
                                ];
                              return (
                                <Button
                                  key={action}
                                  variant="ghost"
                                  size="icon-sm"
                                  aria-label={`${t('admin.resourceType.mcp')} ${t(`admin.permissionType.${action}`)}`}
                                  aria-pressed={checked}
                                  title={t(`admin.permissionType.${action}`)}
                                  className={
                                    checked
                                      ? 'text-accent-primary hover:text-accent-primary'
                                      : 'text-text-secondary'
                                  }
                                  onClick={() =>
                                    handleToggle(
                                      'mcp',
                                      action,
                                      !checked,
                                      resource.actions,
                                    )
                                  }
                                >
                                  <Icon />
                                </Button>
                              );
                            },
                          )}
                        <Switch
                          checked={enabled}
                          aria-label={t(
                            `admin.resourceType.${resource.resource_type}`,
                          )}
                          onCheckedChange={(value) =>
                            handleToggle(
                              resource.resource_type,
                              'enable',
                              value === true,
                              resource.actions,
                            )
                          }
                        />
                      </div>
                      {isProvider && expanded && (
                        <div className="mt-4 border-t border-border-button pt-4">
                          <p className="mb-3 text-sm text-text-secondary">
                            {t('permission.modelProvidersDescription')}
                          </p>
                          <div className="mb-3 flex flex-wrap items-center gap-3">
                            <label className="flex items-center gap-2 text-sm text-text-secondary">
                              <Checkbox
                                checked={
                                  someProvidersSelected
                                    ? 'indeterminate'
                                    : allProvidersSelected
                                }
                                disabled={
                                  !enabled || filteredProviderNames.length === 0
                                }
                                onCheckedChange={(value) => {
                                  const checked = value === true;
                                  setPermissionsDraft((current) => {
                                    const next = { ...current };
                                    filteredProviderNames.forEach((name) => {
                                      next[providerPermissionKey(name)] = {
                                        enable: checked,
                                      };
                                    });
                                    return next;
                                  });
                                }}
                              />
                              {t('common.selectAll')}
                            </label>
                            <div className="relative w-full max-w-xs">
                              <Search className="absolute left-3 top-1/2 size-4 -translate-y-1/2 text-text-secondary" />
                              <Input
                                value={providerSearch}
                                onChange={(event) =>
                                  setProviderSearch(event.target.value)
                                }
                                placeholder={t('common.search')}
                                className="pl-9"
                              />
                            </div>
                          </div>
                          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-5">
                            {filteredProviderNames.map((providerName) => {
                              const key = providerPermissionKey(providerName);
                              return (
                                <label
                                  key={providerName}
                                  className="flex min-w-0 items-center gap-3 rounded-xl bg-bg-card px-4 py-3 text-sm text-text-primary transition-colors hover:bg-bg-input"
                                >
                                  <Checkbox
                                    checked={
                                      displayPermissions[key]?.enable === true
                                    }
                                    disabled={!enabled}
                                    aria-label={providerName}
                                    onCheckedChange={(value) =>
                                      handleToggle(
                                        key,
                                        'enable',
                                        value === true,
                                        ['enable'],
                                      )
                                    }
                                  />
                                  <LlmIcon
                                    name={providerName}
                                    imgClass="size-8"
                                  />
                                  <span className="min-w-0 truncate">
                                    {providerName}
                                  </span>
                                </label>
                              );
                            })}
                          </div>
                        </div>
                      )}
                      {isDataSource && expanded && (
                        <>
                          <p className="mt-4 text-sm text-text-secondary">
                            {t('permission.dataSourcesDescription')}
                          </p>
                          <IntegrationItemPicker
                            items={dataSourceItems}
                            selected={(id) =>
                              displayPermissions[dataSourcePermissionKey(id)]
                                ?.enable === true
                            }
                            enabled={enabled}
                            onToggle={(id, checked) =>
                              handleToggle(
                                dataSourcePermissionKey(id),
                                'enable',
                                checked,
                                ['enable'],
                              )
                            }
                          />
                        </>
                      )}
                      {isChatChannel && expanded && (
                        <>
                          <p className="mt-4 text-sm text-text-secondary">
                            {t('permission.chatChannelsDescription')}
                          </p>
                          <IntegrationItemPicker
                            items={chatChannelItems}
                            selected={(id) =>
                              displayPermissions[chatChannelPermissionKey(id)]
                                ?.enable === true
                            }
                            enabled={enabled}
                            onToggle={(id, checked) =>
                              handleToggle(
                                chatChannelPermissionKey(id),
                                'enable',
                                checked,
                                ['enable'],
                              )
                            }
                          />
                        </>
                      )}
                    </section>
                  );
                })}
              </div>
            </div>
          )}
        </TabsContent>
      </Tabs>

      <div className="mt-auto flex shrink-0 justify-end gap-3 pt-8">
        <Button variant="outline" onClick={handleBack}>
          {t('common.cancel')}
        </Button>
        <Button
          loading={savingPermissions}
          disabled={
            loadingResources ||
            loadingPermissions ||
            resourcesError ||
            permissionsError ||
            !permissionsResult
          }
          onClick={handleSavePermissions}
        >
          {t('common.confirm')}
        </Button>
      </div>
    </section>
  );
}
