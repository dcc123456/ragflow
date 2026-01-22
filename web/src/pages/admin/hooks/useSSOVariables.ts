import { useMutation } from '@tanstack/react-query';
import { chain, difference, keyBy, mapKeys, mapValues } from 'lodash';
import { useCallback, useMemo } from 'react';

import { deleteLdapServer } from '@/services/admin-service';

import { type SchemaType as SSOLDAPSchemaType } from '../sso-providers/ldap-list';
import { uuidParts } from '../utils';
import useAdminVariables from './useAdminVariables';

const SSO_IDP_PREFIX = ['google', 'github', 'feishu'] as const;
const SSO_LDAP_PREFIX = ['ldap'] as const;
const SSO_PREFIX = [...SSO_IDP_PREFIX, ...SSO_LDAP_PREFIX] as const;
const SSO_ID_SEPARATOR = '|';
const SSO_VARNAME_SEPARATOR = '.';
const SSO_LDAP_EXTRACTOR_REGEX = new RegExp(
  `^(?:${SSO_LDAP_PREFIX.join('|')})\\${SSO_ID_SEPARATOR}(.+)\\${SSO_VARNAME_SEPARATOR}(?:.+)$`,
);

export type IdpData =
  | AdminService.SystemVariables.SSO.IDP.Google
  | AdminService.SystemVariables.SSO.IDP.GitHub
  | AdminService.SystemVariables.SSO.IDP.Feishu;

export type GroupedIDPVariables = {
  google: AdminService.SystemVariables.SSO.IDP.Google;
  github: AdminService.SystemVariables.SSO.IDP.GitHub;
  feishu: AdminService.SystemVariables.SSO.IDP.Feishu;
};

export type GroupedLDAPVariables = {
  [x: string]: AdminService.SystemVariables.SSO.LDAP;
};

export type GroupedSSOVariables = GroupedIDPVariables & {
  ldap: GroupedLDAPVariables;
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
      } as unknown as AdminService.SetVariablesInput);
    },
  });

  const { mutateAsync: disable, isPending: isDisabling } = useMutation({
    mutationKey: ['admin/disableLdapServer', id],
    mutationFn: async () => {
      return setVariables({
        [`ldap|${id}.enabled`]: false,
      } as unknown as AdminService.SetVariablesInput);
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
      } as unknown as AdminService.SetVariablesInput);
    },
  });

  const { mutateAsync: disable, isPending: isDisabling } = useMutation({
    mutationKey: ['admin/disableIdpProvider', id],
    mutationFn: async () => {
      return setVariables({
        [`${id}|sso.enabled`]: false,
      } as unknown as AdminService.SetVariablesInput);
    },
    onSuccess: () => {
      refetch();
    },
  });

  const { mutateAsync: update, isPending: isUpdating } = useMutation({
    mutationKey: ['admin/updateIdpProvider', id],
    mutationFn: async (data: GroupedIDPVariables) => {
      return setVariables(
        mapKeys(
          data,
          (_, key) => `${id}|sso.${key}`,
        ) as unknown as AdminService.SetVariablesInput,
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
          key.startsWith(`${prefix}${SSO_ID_SEPARATOR}`),
        ),
      )
      .groupBy((value) => value.name.split(SSO_ID_SEPARATOR)[0])
      .value() as unknown as {
      ldap: ValueOf<AdminService.SystemVariables.SSO.LDAP>[];
      google: ValueOf<AdminService.SystemVariables.SSO.IDP.Google>[];
      github: ValueOf<AdminService.SystemVariables.SSO.IDP.GitHub>[];
      feishu: ValueOf<AdminService.SystemVariables.SSO.IDP.Feishu>[];
    };

    const ssoLdapVars = chain(_ssoLdapVars ?? [])
      .groupBy((value) => value.name.match(SSO_LDAP_EXTRACTOR_REGEX)![1])
      .mapValues((group) => keyBy(group, (value) => value.name.split('.')[1]!))
      .value() as GroupedLDAPVariables;

    const ssoIdpVars = mapValues(_ssoIdpVars ?? {}, (array) =>
      // @ts-ignore
      keyBy(array, (value) => value.name.split(SSO_VARNAME_SEPARATOR)[1]!),
    ) as GroupedIDPVariables;

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
