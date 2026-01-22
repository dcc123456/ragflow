import { useMutation } from '@tanstack/react-query';
import { chain, difference, mapKeys } from 'lodash';
import { useCallback, useMemo } from 'react';

import { deleteLdapServer } from '@/services/admin-service';

import { type SchemaType as SSOLDAPSchemaType } from '../sso-providers/ldap-list';
import { uuidParts } from '../utils';
import useAdminVariables from './useAdminVariables';

const SSO_IDP_PREFIX = ['google', 'github', 'feishu'] as const;
const SSO_LDAP_PREFIX = ['ldap'] as const;
const SSO_PREFIX = [...SSO_IDP_PREFIX, ...SSO_LDAP_PREFIX] as const;
const SSO_NAME_SEPARATOR = '|';
const VARIABLE_FIELD_NAME_SEPARATOR =
  '.' as AdminService.SystemVariables.NameSeparator;

type SSOIDPGoogleData = UnprefixKeys<
  AdminService.SystemVariables.RetypeByTypeAnnotation<AdminService.SystemVariables.SSO.Google>,
  `${AdminService.SystemVariables.SSO.GoogleFieldNamePrefix}${AdminService.SystemVariables.NameSeparator}`
>;

type SSOIDPGitHubData = UnprefixKeys<
  AdminService.SystemVariables.RetypeByTypeAnnotation<AdminService.SystemVariables.SSO.GitHub>,
  `${AdminService.SystemVariables.SSO.GitHubFieldNamePrefix}${AdminService.SystemVariables.NameSeparator}`
>;

type SSOIDPFeishuData = UnprefixKeys<
  AdminService.SystemVariables.RetypeByTypeAnnotation<AdminService.SystemVariables.SSO.Feishu>,
  `${AdminService.SystemVariables.SSO.FeishuFieldNamePrefix}${AdminService.SystemVariables.NameSeparator}`
>;
export type IdpData = SSOIDPGoogleData | SSOIDPGitHubData | SSOIDPFeishuData;

export type GroupedIDPVariables = {
  google: SSOIDPGoogleData;
  github: SSOIDPGitHubData;
  feishu: SSOIDPFeishuData;
};

export type SSOLDAPData = UnprefixKeys<
  AdminService.SystemVariables.RetypeByTypeAnnotation<AdminService.SystemVariables.SSO.LDAP>,
  `${AdminService.SystemVariables.SSO.LDAPFieldNamePrefix}${AdminService.SystemVariables.NameSeparator}`
>;

export type GroupedLDAPVariables = {
  default: SSOLDAPData;
  [x: string]: SSOLDAPData;
};

export type ProviderType = 'none' | 'idp' | 'ldap';

export function useAddLdapServer() {
  const { refetch, setVariables } = useAdminVariables();

  const { mutateAsync: add, isPending: isAdding } = useMutation({
    mutationKey: ['admin/addLdapServer'],
    mutationFn: async (data: SSOLDAPSchemaType) => {
      const id = `server-${uuidParts()[0]}`;
      return setVariables(
        mapKeys(
          { enabled: true, ...data },
          (_, key) => `ldap|${id}.${key}`,
        ) as AdminService.SetVariablesInput,
      );
    },
    onSuccess: () => {
      refetch();
    },
  });

  return {
    add,
    isAdding,
  };
}

export function useMutateLdapServer(id: string) {
  const { refetch, setVariables } = useAdminVariables();

  const { mutateAsync: enable, isPending: isEnabling } = useMutation({
    mutationKey: ['admin/enableLdapServer', id],
    mutationFn: async () => {
      return setVariables({
        [`ldap|${id}.enabled`]: true,
      } as AdminService.SetVariablesInput);
    },
  });

  const { mutateAsync: disable, isPending: isDisabling } = useMutation({
    mutationKey: ['admin/disableLdapServer', id],
    mutationFn: async () => {
      return setVariables({
        [`ldap|${id}.enabled`]: false,
      } as AdminService.SetVariablesInput);
    },
    onSuccess: () => {
      refetch();
    },
  });

  const { mutateAsync: update, isPending: isUpdating } = useMutation({
    mutationKey: ['admin/updateLdapServer', id],
    mutationFn: async (data: SSOLDAPSchemaType) => {
      return setVariables(
        mapKeys(
          data,
          (_, key) => `ldap|${id}.${key}`,
        ) as AdminService.SetVariablesInput,
      );
    },
    onSuccess: () => {
      refetch();
    },
  });

  const { mutateAsync: _delete, isPending: isDeleting } = useMutation({
    mutationKey: ['admin/deleteLdapServer', id],
    mutationFn: async () => {
      // Disallow deleting the default LDAP server
      if (id !== 'default') {
        return deleteLdapServer(id);
      }

      return true;
    },
    onSuccess: () => {
      refetch();
    },
  });

  return {
    enable,
    disable,
    update,
    delete: _delete,
    isUpdating,
    isDeleting,
    isSwitchingState: isEnabling || isDisabling,
  };
}

export function useMutateIdpProvider(
  id: AdminService.SystemVariables.SSO.IDP.ProviderId,
) {
  const { refetch, setVariables } = useAdminVariables();

  const { mutateAsync: enable, isPending: isEnabling } = useMutation({
    mutationKey: ['admin/enableIdpProvider', id],
    mutationFn: async () => {
      return setVariables({
        [`${id}|sso.enabled`]: true,
      } as AdminService.SetVariablesInput);
    },
  });

  const { mutateAsync: disable, isPending: isDisabling } = useMutation({
    mutationKey: ['admin/disableIdpProvider', id],
    mutationFn: async () => {
      return setVariables({
        [`${id}|sso.enabled`]: false,
      } as AdminService.SetVariablesInput);
    },
    onSuccess: () => {
      refetch();
    },
  });

  const { mutateAsync: update, isPending: isUpdating } = useMutation({
    mutationKey: ['admin/updateIdpProvider', id],
    mutationFn: async (
      data: AdminService.SSOIDPSettings[keyof AdminService.SSOIDPSettings],
    ) => {
      return setVariables(
        mapKeys(
          data,
          (_, key) => `${id}|sso.${key}`,
        ) as AdminService.SetVariablesInput,
      );
    },
    onSuccess: () => {
      refetch();
    },
  });

  return {
    enable,
    disable,
    update,
    isUpdating,
    isSwitchingState: isEnabling || isDisabling,
  };
}

export function useSSOVariables() {
  const {
    variables: allVariables,
    isFetching,
    refetch,
    setVariables,
    isUpdating,
  } = useAdminVariables();

  const ssoVariables = useMemo(() => {
    const { ldap: _ssoLdapVars, ..._ssoIdpVars } = chain(allVariables ?? {})
      .pickBy((_, key) =>
        SSO_PREFIX.some((prefix) =>
          key.startsWith(`${prefix}${SSO_NAME_SEPARATOR}`),
        ),
      )
      .groupBy((value) => value.name.split(SSO_NAME_SEPARATOR)[0])
      .value();

    const ssoLdapVars = chain(_ssoLdapVars ?? [])
      .filter((value) => {
        // Filter out invalid entries
        const parts = value.name.split(SSO_NAME_SEPARATOR);
        return (
          parts.length >= 2 && parts[1].includes(VARIABLE_FIELD_NAME_SEPARATOR)
        );
      })
      .groupBy((value) => {
        // Parse 'ldap|<name>.<attrName>' to extract <name>
        const parts = value.name.split(SSO_NAME_SEPARATOR);
        const nameAndAttr = parts[1]!.split(VARIABLE_FIELD_NAME_SEPARATOR);
        return nameAndAttr[0]!;
      })
      .mapValues((group) => {
        // Transform each group into a dictionary keyed by <attrName>
        return chain(group)
          .keyBy((value) => {
            // Parse 'ldap|<name>.<attrName>' to extract <attrName>
            const parts = value.name.split(SSO_NAME_SEPARATOR);
            const nameAndAttr = parts[1]!.split(VARIABLE_FIELD_NAME_SEPARATOR);
            return nameAndAttr[1]!;
          })
          .value();
      })
      .value() as GroupedLDAPVariables;

    const ssoIdpVars = chain(_ssoIdpVars ?? {})
      .mapValues((array) => {
        // Transform each array into a dictionary keyed by <attrName>
        // Format: '<name>|sso.<attrName>' -> extract <attrName>
        return chain(array)
          .filter((value) => {
            // Filter out invalid entries
            const parts = value.name.split(SSO_NAME_SEPARATOR);
            return (
              parts.length >= 2 &&
              parts[1]!.startsWith('sso' + VARIABLE_FIELD_NAME_SEPARATOR)
            );
          })
          .keyBy((value) => {
            // Parse '<name>|sso.<attrName>' to extract <attrName>
            const parts = value.name.split(SSO_NAME_SEPARATOR);
            const ssoAndAttr = parts[1]!.split(VARIABLE_FIELD_NAME_SEPARATOR);
            return ssoAndAttr[1]!;
          })
          .value();
      })
      .value() as GroupedIDPVariables;

    return {
      ldap: ssoLdapVars,
      ...ssoIdpVars,
    };
  }, [allVariables]);

  const providerType: ProviderType = useMemo(() => {
    const idpEnabled =
      ssoVariables.google?.enabled.value ||
      ssoVariables.github?.enabled.value ||
      ssoVariables.feishu?.enabled.value;

    const ldapEnabled = Object.values(ssoVariables.ldap).some(
      (value) => value.enabled.value,
    );

    return idpEnabled ? 'idp' : ldapEnabled ? 'ldap' : 'none';
  }, [
    ssoVariables.google?.enabled.value,
    ssoVariables.github?.enabled.value,
    ssoVariables.feishu?.enabled.value,
    ssoVariables.ldap,
  ]);

  const switchProviderType = useCallback(
    (type?: ProviderType) => {
      const idpNames = ['github|sso', 'google|sso', 'feishu|sso'];
      const ldapNames = Object.keys(ssoVariables.ldap).map((n) => `ldap|${n}`);
      const allNames = [...idpNames, ...ldapNames];

      const enableNames =
        type === 'idp'
          ? ['github|sso', 'google|sso', 'feishu|sso'] // Auto expand to all SSO providers
          : type === 'ldap'
            ? ldapNames // Auto expand to all LDAP servers
            : [];

      const disableNames = difference(allNames, enableNames);

      return setVariables(
        Object.fromEntries([
          ...disableNames.map((name) => [`${name}.enabled`, false]),
          ...enableNames.map((name) => [`${name}.enabled`, true]),
        ]) as AdminService.SetVariablesInput,
      );
    },
    [setVariables, ssoVariables.ldap],
  );

  return {
    variables: ssoVariables,
    providerType,
    switchProviderType,
    refetch,
    isFetching,
    isUpdating,
  };
}
