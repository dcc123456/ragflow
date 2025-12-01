import { TeamManagement } from './team-management';

import styles from './index.less';

const UserSettingTeam = () => {
  return (
    <div className={styles.teamWrapper}>
      <TeamManagement />
    </div>
  );
};

export default UserSettingTeam;
