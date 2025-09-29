import styles from './index.less';
import { TeamManagement } from './team-management';

const UserSettingTeam = () => {
  return (
    <div className={styles.teamWrapper}>
      <TeamManagement></TeamManagement>
    </div>
  );
};

export default UserSettingTeam;
