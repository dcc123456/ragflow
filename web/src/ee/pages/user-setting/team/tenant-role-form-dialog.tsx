import message from '@/components/ui/message';
import { ITenantRole } from '@/ee/interfaces/database/team';
import { useTranslation } from 'react-i18next';
import { RoleFormDialog } from './role-form-dialog';
import {
  useCreateTenantRole,
  useUpdateTenantRole,
} from './use-tenant-role-management';

export function TenantRoleFormDialog({
  tenantId,
  role,
  onClose,
}: {
  tenantId: string;
  role?: ITenantRole;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const { loading: creating, createRole } = useCreateTenantRole(tenantId);
  const { loading: updating, updateRole } = useUpdateTenantRole(
    tenantId,
    role?.id ?? 0,
  );

  const handleSubmit = async (values: {
    roleName: string;
    description: string;
  }) => {
    const payload = {
      role_name: values.roleName,
      description: values.description,
    };
    const code = role ? await updateRole(payload) : await createRole(payload);

    if (code === 0) {
      message.success(
        t(role ? 'permission.roleUpdated' : 'permission.roleCreated'),
      );
      onClose();
    }
  };

  return (
    <RoleFormDialog
      roleName={role?.role_name}
      description={role?.description}
      loading={creating || updating}
      onClose={onClose}
      onSubmit={(values) => void handleSubmit(values)}
    />
  );
}
