import { useMutation } from '@tanstack/react-query';
import { chain, difference, identity, isEmpty, mapKeys } from 'lodash';
import { useCallback, useMemo } from 'react';

import { deleteLdapServer } from '@/ee/services/admin-service';

import { type SchemaType as SSOLDAPSchemaType } from '@/ee/pages/admin/sso-providers/ldap-list';
import { uuidParts } from '@/ee/pages/admin/utils';
import useAdminVariables from '@/ee/pages/admin/hooks/useAdminVariables';

const SSO_ID_SEPARATOR = '|';
const SSO_VARNAME_SEPARATOR = '.';

type SSORegExpExecGroups = {
  groups: {
    id: string;
    type: 'sso' | 'ldap' | 'cas';
    varName: string;
  };
};

const SSO_IDP_EXTRACTOR_REGEX = new RegExp(
  `^(?<id>.+?)\\${SSO_ID_SEPARATOR}(?<type>sso)\\${SSO_VARNAME_SEPARATOR}(?<varName>.+)$`,
);

const SSO_LDAP_EXTRACTOR_REGEX = new RegExp(
  `^(?<type>ldap)\\${SSO_ID_SEPARATOR}(?<id>.+?)\\${SSO_VARNAME_SEPARATOR}(?<varName>.+)$`,
);

const CASChannelPattern = /^cas(?:-[0-9a-f]{32})?$/;

export type IdpData =
  | AdminServiceEe.SystemVariables.SSO.IDP.Google
  | AdminServiceEe.SystemVariables.SSO.IDP.GitHub
  | AdminServiceEe.SystemVariables.SSO.IDP.Feishu
  | AdminServiceEe.SystemVariables.SSO.IDP.Wecom
  | AdminServiceEe.SystemVariables.SSO.IDP.Dingtalk;

export type GroupedIDPVariables = {
  google: AdminServiceEe.SystemVariables.SSO.IDP.Google;
  github: AdminServiceEe.SystemVariables.SSO.IDP.GitHub;
  feishu: AdminServiceEe.SystemVariables.SSO.IDP.Feishu;
  wecom: AdminServiceEe.SystemVariables.SSO.IDP.Wecom;
  dingtalk: AdminServiceEe.SystemVariables.SSO.IDP.Dingtalk;
};

export type GroupedLDAPVariables = {
  [x: string]: AdminServiceEe.SystemVariables.SSO.LDAP;
};

export type CASChannel = AdminServiceEe.SystemVariables.SSO.CASChannel;
export type CASProviderVariables =
  Partial<AdminServiceEe.SystemVariables.SSO.CAS>;
export type GroupedCASVariables = Partial<
  Record<CASChannel, CASProviderVariables>
>;

export function hasCASConnectionSettings(provider?: CASProviderVariables) {
  return Boolean(
    provider?.login_url?.value?.trim() &&
    provider?.validation_url?.value?.trim() &&
    provider?.redirect_uri?.value?.trim(),
  );
}

export type GroupedSSOVariables = {
  sso: GroupedIDPVariables;
  ldap: GroupedLDAPVariables;
  cas: GroupedCASVariables;
};

export type ProviderType = 'none' | 'idp' | 'ldap' | 'cas';

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
        ) as AdminServiceEe.SetVariablesInput,
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
      } as unknown as AdminServiceEe.SetVariablesInput);
    },
  });

  const { mutateAsync: disable, isPending: isDisabling } = useMutation({
    mutationKey: ['admin/disableLdapServer', id],
    mutationFn: async () => {
      return setVariables({
        [`ldap|${id}.enabled`]: false,
      } as unknown as AdminServiceEe.SetVariablesInput);
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
        ) as AdminServiceEe.SetVariablesInput,
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
  id: AdminServiceEe.SystemVariables.SSO.IDP.ProviderId | CASChannel,
) {
  const { refetch, setVariables } = useAdminVariables();

  const { mutateAsync: enable, isPending: isEnabling } = useMutation({
    mutationKey: ['admin/enableIdpProvider', id],
    mutationFn: async () => {
      return setVariables({
        [`${id}|sso.enabled`]: true,
      } as unknown as AdminServiceEe.SetVariablesInput);
    },
  });

  const { mutateAsync: disable, isPending: isDisabling } = useMutation({
    mutationKey: ['admin/disableIdpProvider', id],
    mutationFn: async () => {
      return setVariables({
        [`${id}|sso.enabled`]: false,
      } as unknown as AdminServiceEe.SetVariablesInput);
    },
    onSuccess: () => {
      refetch();
    },
  });

  const { mutateAsync: update, isPending: isUpdating } = useMutation({
    mutationKey: ['admin/updateIdpProvider', id],
    mutationFn: async (data: Record<string, string | boolean>) => {
      return setVariables(
        mapKeys(
          data,
          (_, key) => `${id}|sso.${key}`,
        ) as unknown as AdminServiceEe.SetVariablesInput,
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
    if (isEmpty(allVariables)) {
      return {
        sso: {},
        ldap: {},
        cas: {},
      } as GroupedSSOVariables;
    }

    return (
      chain(allVariables)
        .mapValues((variable) => {
          const matchResult = (SSO_IDP_EXTRACTOR_REGEX.exec(variable.name) ??
            SSO_LDAP_EXTRACTOR_REGEX.exec(variable.name)) as
            | (Omit<RegExpExecArray, 'groups'> & SSORegExpExecGroups)
            | null;

          return matchResult
            ? {
                ...matchResult.groups,
                type:
                  matchResult.groups.type === 'sso' &&
                  CASChannelPattern.test(matchResult.groups.id)
                    ? 'cas'
                    : matchResult.groups.type,
                data: variable,
              }
            : null;
        })
        // Pick non-null values (matched variables)
        .pickBy(identity)
        .groupBy('type')
        .mapValues((tg) =>
          chain(tg)
            .groupBy('id')
            .mapValues((group) =>
              chain(group).keyBy('varName').mapValues('data').value(),
            )
            .value(),
        )
        .defaults({ sso: {}, ldap: {}, cas: {} })
        .value() as GroupedSSOVariables
    );
  }, [allVariables]);

  const providerType: ProviderType = useMemo(() => {
    if (
      Object.values(ssoVariables.cas).some(
        (provider) =>
          provider?.enabled?.value && hasCASConnectionSettings(provider),
      )
    ) {
      return 'cas';
    }
    if (
      Object.values(ssoVariables.sso).some(
        (provider) => provider?.enabled?.value,
      )
    ) {
      return 'idp';
    }
    if (
      Object.values(ssoVariables.ldap).some((server) => server?.enabled?.value)
    ) {
      return 'ldap';
    }
    return 'none';
  }, [ssoVariables]);

  const switchProviderType = useCallback(
    (type?: ProviderType, casChannel?: CASChannel) => {
      const idpNames = [
        'github|sso',
        'google|sso',
        'feishu|sso',
        'wecom|sso',
        'dingtalk|sso',
      ];
      const ldapNames = Object.keys(ssoVariables.ldap).map((n) => `ldap|${n}`);
      const nonCASNames = [...idpNames, ...ldapNames];
      const casNames = Object.keys(ssoVariables.cas).map((id) => `${id}|sso`);
      const allNames = [...nonCASNames, ...casNames];

      if (
        type === 'cas' &&
        (!casChannel || !hasCASConnectionSettings(ssoVariables.cas[casChannel]))
      ) {
        return;
      }

      const enableNames =
        type === 'idp'
          ? idpNames
          : type === 'ldap'
            ? ldapNames // Auto expand to all LDAP servers
            : type === 'cas' && casChannel
              ? [`${casChannel}|sso`]
              : [];

      const disableNames = difference(
        type === 'cas' ? nonCASNames : allNames,
        enableNames,
      );

      return setVariables(
        Object.fromEntries([
          ...disableNames.map((name) => [`${name}.enabled`, false]),
          ...enableNames.map((name) => [`${name}.enabled`, true]),
        ]) as AdminServiceEe.SetVariablesInput,
      );
    },
    [setVariables, ssoVariables.cas, ssoVariables.ldap],
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
