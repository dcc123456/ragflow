import { ConfirmDeleteDialog } from '@/components/confirm-delete-dialog';
import ListFilterBar from '@/components/list-filter-bar';
import { Button } from '@/components/ui/button';
import { RAGFlowPagination } from '@/components/ui/ragflow-pagination';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip';
import { useNavigatePage } from '@/hooks/logic-hooks/navigate-hooks';
import { useEnterpriseNavigate } from '@/hooks/use-enterprise-navigate';
import { useCloseTicket, useFetchTickets } from '@/hooks/use-ticket-request';
import { formatDateToLocal } from '@/utils/date';
import { pick } from 'lodash';
import { ArrowLeft, CircleX, MessageCircle, Plus } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { getTicketPriorityI18nKey, getTicketStateI18nKey } from './constants';

export default function TicketsPage() {
  const { t } = useTranslation();
  const { navigateToTicketCreate, navigateToTicketDetail } =
    useEnterpriseNavigate();
  const { navigateToHome } = useNavigatePage();

  const {
    tickets,
    pagination,
    setPagination,
    searchString,
    handleInputChange,
    loading,
  } = useFetchTickets();

  const { closeTicket, loading: closing } = useCloseTicket();

  return (
    <section className="p-8">
      <header className="mb-4">
        <ListFilterBar
          showFilter={false}
          searchString={searchString}
          onSearchChange={handleInputChange}
          leftPanel={
            <div className="flex items-center gap-2 mb-6">
              <Button variant="ghost" size="sm" onClick={navigateToHome}>
                <ArrowLeft className="size-4" />
              </Button>
              <h1 className="text-2xl font-bold">{t('tickets.title')}</h1>
            </div>
          }
        >
          <Button onClick={navigateToTicketCreate}>
            <Plus className="size-[1em]" />
            {t('tickets.createTicket')}
          </Button>
        </ListFilterBar>
      </header>

      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>ID</TableHead>
            <TableHead>{t('tickets.title')}</TableHead>
            <TableHead>{t('tickets.state')}</TableHead>
            <TableHead>{t('tickets.priority')}</TableHead>
            <TableHead>{t('tickets.group')}</TableHead>
            <TableHead>{t('tickets.customer')}</TableHead>
            <TableHead>{t('tickets.createdAt')}</TableHead>
            <TableHead className="w-[100px]">{t('common.action')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {loading ? (
            <TableRow>
              <TableCell colSpan={8} className="text-center">
                {'Loading...'}
              </TableCell>
            </TableRow>
          ) : tickets.length === 0 ? (
            <TableRow>
              <TableCell colSpan={8} className="text-center">
                {t('common.noData')}
              </TableCell>
            </TableRow>
          ) : (
            tickets.map((ticket) => (
              <TableRow key={ticket.id}>
                <TableCell>{ticket.id}</TableCell>
                <TableCell className="font-medium">{ticket.title}</TableCell>
                <TableCell>
                  {(() => {
                    const key = getTicketStateI18nKey(ticket.state);
                    return key ? t(key) : ticket.state || '-';
                  })()}
                </TableCell>
                <TableCell>
                  {(() => {
                    const key = getTicketPriorityI18nKey(ticket.priority);
                    return key ? t(key) : ticket.priority || '-';
                  })()}
                </TableCell>
                <TableCell>{ticket.group}</TableCell>
                <TableCell>{ticket.customer}</TableCell>
                <TableCell>
                  {ticket.created_at
                    ? formatDateToLocal(ticket.created_at)
                    : '-'}
                </TableCell>
                <TableCell>
                  <TooltipProvider delayDuration={0}>
                    <div className="flex items-center gap-2">
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <Button
                            variant="ghost"
                            size="sm"
                            className="px-1"
                            onClick={navigateToTicketDetail(ticket.id)}
                          >
                            <MessageCircle className="size-4 text-text-primary" />
                          </Button>
                        </TooltipTrigger>
                        <TooltipContent>
                          {t('tickets.viewConversation')}
                        </TooltipContent>
                      </Tooltip>

                      {ticket.state !== 'closed' &&
                        ticket.state !== 'Closed' && (
                          <ConfirmDeleteDialog
                            title={t('tickets.closeTitle')}
                            content={{
                              title: t('tickets.closeConfirmMessage'),
                              node: undefined,
                            }}
                            okButtonText={t('common.confirm')}
                            cancelButtonText={t('common.cancel')}
                            onOk={async () => {
                              await closeTicket(ticket.id);
                            }}
                          >
                            <Button
                              variant="ghost"
                              size="sm"
                              className="px-1"
                              disabled={closing}
                            >
                              <CircleX className="size-4 text-state-error" />
                            </Button>
                          </ConfirmDeleteDialog>
                        )}
                    </div>
                  </TooltipProvider>
                </TableCell>
              </TableRow>
            ))
          )}
        </TableBody>
      </Table>

      {pagination.total > 0 && (
        <footer className="mt-4 px-5 pb-5">
          <RAGFlowPagination
            {...pick(pagination, 'current', 'pageSize')}
            total={pagination.total}
            onChange={(page, pageSize) => {
              setPagination({ page, pageSize });
            }}
          />
        </footer>
      )}
    </section>
  );
}
