import { ConfirmDeleteDialog } from '@/components/confirm-delete-dialog';
import { RAGFlowAvatar } from '@/components/ragflow-avatar';
import { Button } from '@/components/ui/button';
import { useEnterpriseNavigate } from '@/hooks/use-enterprise-navigate';
import {
  useCloseTicket,
  useFetchTicketArticles,
  useFetchTicketDetail,
  useReplyTicket,
} from '@/hooks/use-ticket-request';
import { useFetchUserInfo } from '@/hooks/use-user-setting-request';
import { formatDateToLocal } from '@/utils/date';
import { fileToBase64, formatBytes } from '@/utils/file-util';
import {
  ArrowLeft,
  CircleX,
  Download,
  FileText,
  ImageIcon,
} from 'lucide-react';
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useParams } from 'react-router';
import { TicketChatInput } from '../chat-input';
import { getTicketPriorityI18nKey, getTicketStateI18nKey } from '../constants';

function isImageMimeType(mimeType?: string): boolean {
  return !!mimeType && mimeType.startsWith('image/');
}

interface AttachmentListProps {
  ticketId: number;
  articleId: number;
  attachments?: {
    id: number;
    filename: string;
    size: number;
    preferences?: {
      'Content-Type'?: string;
      'content-type'?: string;
    };
  }[];
}

function AttachmentList({
  ticketId,
  articleId,
  attachments,
}: AttachmentListProps) {
  const [imageUrls, setImageUrls] = useState<Record<number, string>>({});

  useEffect(() => {
    if (!attachments) return;

    const abortControllers: AbortController[] = [];

    attachments.forEach((att) => {
      const mimeType =
        att.preferences?.['Content-Type'] ||
        att.preferences?.['content-type'] ||
        'application/octet-stream';
      if (!isImageMimeType(mimeType)) return;

      const controller = new AbortController();
      abortControllers.push(controller);

      const url = `/v1/ticket/${ticketId}/articles/${articleId}/attachments/${att.id}`;
      fetch(url, {
        headers: { Authorization: localStorage.getItem('Authorization') || '' },
        signal: controller.signal,
      })
        .then((res) => res.blob())
        .then((blob) => {
          setImageUrls((prev) => ({
            ...prev,
            [att.id]: URL.createObjectURL(blob),
          }));
        })
        .catch(() => {});
    });

    return () => {
      abortControllers.forEach((c) => c.abort());
      Object.values(imageUrls).forEach((url) => URL.revokeObjectURL(url));
    };
  }, [attachments, ticketId, articleId]);

  const handleDownload = (att: {
    id: number;
    filename: string;
    preferences?: { 'Content-Type'?: string; 'content-type'?: string };
  }) => {
    const url = `/v1/ticket/${ticketId}/articles/${articleId}/attachments/${att.id}`;
    fetch(url, {
      headers: { Authorization: localStorage.getItem('Authorization') || '' },
    })
      .then((res) => res.blob())
      .then((blob) => {
        const objectUrl = URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = objectUrl;
        a.download = att.filename;
        document.body.appendChild(a);
        a.click();
        document.body.removeChild(a);
        URL.revokeObjectURL(objectUrl);
      })
      .catch(() => {});
  };

  if (!attachments || attachments.length === 0) return null;

  return (
    <div className="flex flex-wrap gap-2 mt-2">
      {attachments.map((att) => {
        const mimeType =
          att.preferences?.['Content-Type'] ||
          att.preferences?.['content-type'] ||
          'application/octet-stream';
        const isImage = isImageMimeType(mimeType);
        const imgUrl = imageUrls[att.id];

        if (isImage && imgUrl) {
          return (
            <button
              key={att.id}
              type="button"
              onClick={() => handleDownload(att)}
              className="block rounded-md border border-border-divider overflow-hidden hover:opacity-80 transition-opacity"
            >
              <img
                src={imgUrl}
                alt={att.filename}
                className="max-w-[200px] max-h-[150px] object-cover"
              />
            </button>
          );
        }

        return (
          <Button
            key={att.id}
            type="button"
            variant={'outline'}
            onClick={() => handleDownload(att)}
          >
            {isImage ? (
              <ImageIcon className="size-4 text-accent-primary" />
            ) : (
              <FileText className="size-4 text-text-secondary" />
            )}
            <span className="text-xs max-w-[150px] truncate">
              {att.filename}
            </span>
            <span className="text-xs text-text-secondary">
              {formatBytes(att.size)}
            </span>
            <Download className="size-3 text-text-secondary" />
          </Button>
        );
      })}
    </div>
  );
}

export default function TicketDetailPage() {
  const { t } = useTranslation();
  const { navigateToTickets } = useEnterpriseNavigate();
  const { id } = useParams<{ id: string }>();
  const ticketId = Number(id) || 0;

  const { data: ticket, loading: detailLoading } =
    useFetchTicketDetail(ticketId);
  const { data: articles, loading: articlesLoading } =
    useFetchTicketArticles(ticketId);
  const { replyTicket, loading: replying } = useReplyTicket(ticketId);
  const { closeTicket, loading: closing } = useCloseTicket();
  const { data: userInfo } = useFetchUserInfo();

  const [replyBody, setReplyBody] = useState('');
  const [replyFiles, setReplyFiles] = useState<File[]>([]);

  const isClosed = ticket?.state?.toLowerCase() === 'closed';

  const stateKey = getTicketStateI18nKey(ticket?.state);
  const stateLabel = stateKey ? t(stateKey) : ticket?.state || '-';
  const priorityKey = getTicketPriorityI18nKey(ticket?.priority);
  const priorityLabel = priorityKey ? t(priorityKey) : ticket?.priority || '-';

  const handleSendReply = async () => {
    if (!replyBody.trim() && replyFiles.length === 0) return;

    const attachments = await Promise.all(
      replyFiles.map(async (file) => ({
        filename: file.name,
        data: await fileToBase64(file),
        'mime-type': file.type || 'application/octet-stream',
      })),
    );

    await replyTicket({
      body: replyBody,
      attachments: attachments.length > 0 ? attachments : undefined,
    });
    setReplyBody('');
    setReplyFiles([]);
  };

  const stripHtml = (html: string) => {
    const tmp = document.createElement('div');
    tmp.innerHTML = html;
    return tmp.textContent || tmp.innerText || '';
  };

  const getSenderName = (article: (typeof articles)[number]) => {
    const isCustomer = article.sender?.toLowerCase() === 'customer';
    if (isCustomer) {
      return userInfo?.nickname || userInfo?.email || '-';
    }
    return article.from || article.origin_by || article.created_by || '-';
  };

  return (
    <section className="p-8 flex flex-col h-[calc(100vh-60px)]">
      <div className="flex items-center gap-3 mb-4 shrink-0">
        <Button variant="ghost" size="sm" onClick={navigateToTickets}>
          <ArrowLeft className="size-4" />
        </Button>
        <h1 className="text-2xl font-bold">{t('tickets.myTicket')}</h1>
        {!isClosed && (
          <ConfirmDeleteDialog
            title={t('tickets.closeTitle')}
            content={{
              title: t('tickets.closeConfirmMessage'),
            }}
            okButtonText={t('common.confirm')}
            cancelButtonText={t('common.cancel')}
            onOk={async () => {
              await closeTicket(ticketId);
              navigateToTickets();
            }}
          >
            <Button
              variant="destructive"
              size="sm"
              disabled={closing}
              loading={closing}
              className="ml-auto"
            >
              <CircleX className="size-4 mr-1" />
              {t('tickets.closeTitle')}
            </Button>
          </ConfirmDeleteDialog>
        )}
      </div>

      <div className="bg-accent-primary text-white rounded-md px-6 py-3 mb-4 flex flex-wrap gap-4 items-center shrink-0">
        <span>
          {t('tickets.detail.subject')}: {ticket?.title}
        </span>
        <span>
          {t('tickets.detail.group')}: {ticket?.group}
        </span>
        <span>
          {t('tickets.detail.priority')}: {priorityLabel}
        </span>
        <span>
          {t('tickets.detail.state')}: {stateLabel}
        </span>
        <span>
          {t('tickets.detail.updatedAt')}:{' '}
          {ticket?.updated_at ? formatDateToLocal(ticket.updated_at) : '-'}
        </span>
      </div>

      <div className="flex-1 overflow-auto min-h-0 mb-4 pr-2 space-y-4">
        {detailLoading || articlesLoading ? (
          <div className="text-center text-muted-foreground py-10">
            Loading...
          </div>
        ) : (
          articles.map((article) => {
            const isCustomer = article.sender?.toLowerCase() === 'customer';
            return (
              <div
                key={article.id}
                className={`flex gap-3 ${
                  isCustomer ? 'justify-end' : 'justify-start'
                }`}
              >
                {!isCustomer && (
                  <RAGFlowAvatar
                    className="w-8 h-8 shrink-0"
                    name={getSenderName(article)}
                    isPerson
                  />
                )}
                <div className={`max-w-[70%] rounded-lg p-4 bg-bg-card`}>
                  <div className="flex items-center justify-between gap-4 mb-2">
                    <span className="text-sm font-medium text-accent-primary">
                      {article.created_at
                        ? formatDateToLocal(article.created_at)
                        : '-'}
                    </span>
                    <span className="text-xs px-2 py-0.5 rounded bg-bg-card text-text-primary">
                      {getSenderName(article)}
                    </span>
                  </div>
                  <div className="text-sm whitespace-pre-wrap">
                    {stripHtml(article.body)}
                  </div>
                  <AttachmentList
                    ticketId={ticketId}
                    articleId={article.id}
                    attachments={article.attachments}
                  />
                </div>
                {isCustomer && (
                  <RAGFlowAvatar
                    className="w-8 h-8 shrink-0"
                    avatar={userInfo?.avatar}
                    name={userInfo?.nickname || userInfo?.email}
                    isPerson
                  />
                )}
              </div>
            );
          })
        )}
      </div>

      {!isClosed && (
        <div className="shrink-0 space-y-1">
          <label className="text-sm font-medium text-text-secondary mb-1 block">
            {t('tickets.detail.replyLabel')}
          </label>
          <TicketChatInput
            value={replyBody}
            onChange={setReplyBody}
            files={replyFiles}
            onFilesChange={setReplyFiles}
            onSend={handleSendReply}
            sending={replying}
            placeholder={t('tickets.detail.replyPlaceholder') ?? undefined}
          />
        </div>
      )}

      {isClosed && (
        <div className="shrink-0 text-center text-sm text-muted-foreground py-2 border-t">
          {t('tickets.detail.closedNotice')}
        </div>
      )}
    </section>
  );
}
