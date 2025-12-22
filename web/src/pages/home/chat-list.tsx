import { HomeCard } from '@/components/home-card';
import { MoreButton } from '@/components/more-button';
import { PrivilegeManagementDialog } from '@/components/privilege-management/privilege-management-dialog';
import { RenameDialog } from '@/components/rename-dialog';
import { useNavigatePage } from '@/hooks/logic-hooks/navigate-hooks';
import { useFetchDialogList } from '@/hooks/use-chat-request';
import { useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { ChatDropdown } from '../next-chats/chat-dropdown';
import { useRenameChat } from '../next-chats/hooks/use-rename-chat';
import { useShowPrivilegeDialog } from '../next-chats/use-show-privilege-dialog';

export function ChatList({
  setListLength,
  setLoading,
}: {
  setListLength: (length: number) => void;
  setLoading?: (loading: boolean) => void;
}) {
  const { t } = useTranslation();
  const { data, loading } = useFetchDialogList();
  const { navigateToChat } = useNavigatePage();

  const {
    initialChatName,
    chatRenameVisible,
    showChatRenameModal,
    hideChatRenameModal,
    onChatRenameOk,
    chatRenameLoading,
  } = useRenameChat();
  useEffect(() => {
    setListLength(data?.dialogs?.length || 0);
    setLoading?.(loading || false);
  }, [data, setListLength, loading, setLoading]);

  const {
    handleShowPrivilegeModal,
    privilegeRecord,
    hidePrivilegeModal,
    privilegeModalVisible,
  } = useShowPrivilegeDialog();

  return (
    <>
      {data.dialogs?.slice(0, 10).map((x) => (
        <HomeCard
          key={x.id}
          data={{
            avatar: x.icon,
            ...x,
          }}
          onClick={navigateToChat(x.id)}
          moreDropdown={
            <ChatDropdown
              chat={x}
              showChatRenameModal={showChatRenameModal}
              showPrivilegeModal={handleShowPrivilegeModal(x)}
            >
              <MoreButton></MoreButton>
            </ChatDropdown>
          }
        ></HomeCard>
      ))}
      {chatRenameVisible && (
        <RenameDialog
          hideModal={hideChatRenameModal}
          onOk={onChatRenameOk}
          initialName={initialChatName}
          loading={chatRenameLoading}
          title={initialChatName || t('chat.createChat')}
        ></RenameDialog>
      )}
      {privilegeModalVisible && (
        <PrivilegeManagementDialog
          hideModal={hidePrivilegeModal}
          initialValues={privilegeRecord}
        ></PrivilegeManagementDialog>
      )}
    </>
  );
}
