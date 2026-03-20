import { Plus } from 'lucide-react';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';

import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';

import { AddLicenseDialog } from './add-license-dialog';
import { useLicense } from './hooks/use-license';

export function License() {
  const { t } = useTranslation();
  const [isDialogOpen, setIsDialogOpen] = useState(false);
  const { license, licenseStatus, isFetching, submitLicense, isSubmitting } =
    useLicense();

  const handleConfirm = async (licenseKey: string) => {
    await submitLicense(licenseKey);
    setIsDialogOpen(false);
  };

  return (
    <section className="flex flex-col gap-6 p-6">
      <div className="flex items-center justify-between pb-6">
        <div className="flex flex-col gap-1.5">
          <h2 className="text-xl font-semibold text-text-primary">
            {t('license.title', 'License')}
          </h2>
          <p className="text-sm text-text-secondary">
            {t(
              'license.description',
              'Manage your licenses and ensure continuous operation.',
            )}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="default"
            onClick={() => setIsDialogOpen(true)}
          >
            <Plus className="size-4 mr-2" />
            {t('license.addLicense', 'Add license')}
          </Button>
        </div>
      </div>
      <LicenseTable
        license={license}
        licenseStatus={licenseStatus}
        isLoading={isFetching}
      />

      <AddLicenseDialog
        open={isDialogOpen}
        onOpenChange={setIsDialogOpen}
        onConfirm={handleConfirm}
        isSubmitting={isSubmitting}
      />
    </section>
  );
}

interface LicenseTableProps {
  license?: AdminService.LicenseInfo;
  licenseStatus: AdminService.LicenseStatus;
  isLoading: boolean;
}

function LicenseTable({
  license,
  licenseStatus,
  isLoading,
}: LicenseTableProps) {
  const { t } = useTranslation();

  const formatDate = (dateString: string) => {
    if (!dateString) return '-';
    try {
      const date = new Date(dateString);
      return date.toLocaleString();
    } catch {
      return dateString;
    }
  };

  if (isLoading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-10 w-full" />
        <Skeleton className="h-16 w-full" />
      </div>
    );
  }

  if (!license) {
    return (
      <div className="flex flex-col items-center justify-center py-12 text-text-secondary">
        <p className="text-sm">
          {t('license.noLicense', 'No license found. Please add a license.')}
        </p>
      </div>
    );
  }

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead className="w-[120px]">
            {t('license.table.id', 'ID')}
          </TableHead>
          <TableHead>{t('license.table.startTime', 'Start time')}</TableHead>
          <TableHead>{t('license.table.expiryTime', 'Expiry time')}</TableHead>
          <TableHead className="w-[100px]">
            {t('license.table.status', 'Status')}
          </TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow>
          <TableCell className="font-medium">{license.ID || '-'}</TableCell>
          <TableCell>{formatDate(license.ValidFrom)}</TableCell>
          <TableCell>{formatDate(license.ValidUntil)}</TableCell>
          <TableCell>
            <StatusBadge status={licenseStatus} />
          </TableCell>
        </TableRow>
      </TableBody>
    </Table>
  );
}

interface StatusBadgeProps {
  status: AdminService.LicenseStatus;
}

function StatusBadge({ status }: StatusBadgeProps) {
  const { t } = useTranslation();

  const statusConfig = {
    valid: {
      label: t('license.status.valid', 'Valid'),
      className: 'bg-accent-primary',
    },
    expired: {
      label: t('license.status.expired', 'Expired'),
      className: 'bg-state-error',
    },
    pending: {
      label: t('license.status.pending', 'Pending'),
      className: 'bg-amber-500',
    },
    inactive: {
      label: t('license.status.inactive', 'Inactive'),
      className: 'bg-gray-500',
    },
  };

  const config = statusConfig[status];

  return (
    <div className="flex items-center gap-2">
      <span
        className={`inline-block size-2 rounded-full ${config.className}`}
      ></span>
      <span className="text-text-primary">{config.label}</span>
    </div>
  );
}
