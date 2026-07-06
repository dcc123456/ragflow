import { HomeCard } from '@/components/home-card';
import { MoreButton } from '@/components/more-button';
import { useNavigatePage } from '@/hooks/logic-hooks/navigate-hooks';
import { ISearchAppProps } from './hooks';
import { SearchDropdown } from './search-dropdown';

interface IProps {
  data: ISearchAppProps;
  showSearchRenameModal: (data: ISearchAppProps) => void;
  showPrivilegeModal(): void;
}
export function SearchCard({
  data,
  showSearchRenameModal,
  showPrivilegeModal,
}: IProps) {
  const { navigateToSearch } = useNavigatePage();

  return (
    <HomeCard
      data={data}
      moreDropdown={
        <SearchDropdown
          dataset={data}
          showSearchRenameModal={showSearchRenameModal}
          showPrivilegeModal={showPrivilegeModal}
        >
          <MoreButton></MoreButton>
        </SearchDropdown>
      }
      onClick={navigateToSearch(data?.id)}
    />
  );
}
