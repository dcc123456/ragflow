import { TableEmpty, TableSkeleton } from '@/components/table-skeleton';
import { TruncatedText } from '@/components/truncated-text';
import { Button } from '@/components/ui/button';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import {
  useAcceptTenantInvitation,
  useDeclineTenantInvitation,
} from '@/ee/layouts/components/team-invitation-reminder-dialog/use-agree-invitation';
import { useListTenant } from '@/hooks/use-user-setting-request';
import { ITenant } from '@/interfaces/database/user-setting';
import { TenantMembershipStatus } from '@/pages/user-setting/constants';
import { useCallback, useMemo } from 'react';
import { useTranslation } from 'react-i18next';

function InvitationRow({
  tenant,
  accepting,
  declining,
  onAccept,
  onDecline,
}: {
  tenant: ITenant;
  accepting: boolean;
  declining: boolean;
  onAccept: (tenantId: string) => void;
  onDecline: (tenantId: string) => void;
}) {
  const { t } = useTranslation();

  const handleAccept = useCallback(() => {
    onAccept(tenant.tenant_id);
  }, [onAccept, tenant.tenant_id]);

  const handleDecline = useCallback(() => {
    onDecline(tenant.tenant_id);
  }, [onDecline, tenant.tenant_id]);

  return (
    <TableRow>
      <TableCell className="max-w-[16vw]">
        <TruncatedText
          tooltip={tenant.nickname || tenant.email}
          className="truncate"
        >
          {tenant.nickname || tenant.email}
        </TruncatedText>
      </TableCell>
      <TableCell className="max-w-[16vw]">
        <TruncatedText tooltip={tenant.email} className="truncate">
          {tenant.email}
        </TruncatedText>
      </TableCell>
      <TableCell>
        <div className="flex items-center gap-1">
          <Button variant="link" loading={accepting} onClick={handleAccept}>
            {t('setting.agree')}
          </Button>
          <Button variant="link" loading={declining} onClick={handleDecline}>
            {t('setting.refuse')}
          </Button>
        </div>
      </TableCell>
    </TableRow>
  );
}

const TenantTable = () => {
  const { t } = useTranslation();
  const { data, loading } = useListTenant();
  const { acceptInvitation, accepting } = useAcceptTenantInvitation();
  const { declineInvitation, declining } = useDeclineTenantInvitation();

  // The Tenant list returns every membership; the reminder only shows the
  // invitations that still wait for the current user's decision.
  const invitations = useMemo(() => {
    return data.filter(
      (tenant) => tenant.status === TenantMembershipStatus.Invited,
    );
  }, [data]);

  const columnsLength = 3;

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t('common.name')}</TableHead>
          <TableHead>{t('setting.email')}</TableHead>
          <TableHead className="w-[140px]">{t('common.action')}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {loading ? (
          <TableSkeleton columnsLength={columnsLength} />
        ) : invitations.length === 0 ? (
          <TableEmpty columnsLength={columnsLength} />
        ) : (
          invitations.map((tenant) => (
            <InvitationRow
              key={tenant.tenant_id}
              tenant={tenant}
              accepting={accepting}
              declining={declining}
              onAccept={acceptInvitation}
              onDecline={declineInvitation}
            />
          ))
        )}
      </TableBody>
    </Table>
  );
};

export default TenantTable;
