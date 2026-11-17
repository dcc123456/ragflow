import ProfilePage from '@/pages/user-setting/profile';
import AccountDeletion from './account-deletion';

export default function EnterpriseProfilePage() {
  return (
    <div className="flex h-full min-h-0 w-full flex-col gap-6">
      <div className="flex min-h-0 flex-1 overflow-y-auto">
        <ProfilePage />
      </div>
      <AccountDeletion />
    </div>
  );
}
