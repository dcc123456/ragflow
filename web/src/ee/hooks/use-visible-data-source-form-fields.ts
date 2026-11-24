import { useTenantMyPermissions } from '@/ee/pages/user-setting/team/use-member-role-query';
import { useFetchTenantInfo } from '@/hooks/use-user-setting-request';

const dataSourceByMode: Record<string, string> = {
  ai_table: 'dingtalk_ai_table',
  wiki: 'dingtalk_wiki',
  drive: 'dingtalk_drive',
};

export function useVisibleDataSourceFormFields(
  sourceID: string | undefined,
  fields: any[],
): any[] {
  const { data: tenantInfo } = useFetchTenantInfo();
  const { data: tenantPermissions } = useTenantMyPermissions(
    tenantInfo.tenant_id,
  );
  const permissions = tenantPermissions?.permissions;

  if (sourceID !== 'dingtalk_ai_table' || !permissions) return fields;

  return fields.map((field) => {
    if (field.name !== 'config.mode' || !Array.isArray(field.options)) {
      return field;
    }
    return {
      ...field,
      options: field.options.filter((option: { value: string }) => {
        const sourceID = dataSourceByMode[option.value];
        return (
          sourceID &&
          permissions.data_source?.enable === true &&
          permissions[`data_source::${sourceID}`]?.enable !== false
        );
      }),
    };
  });
}
