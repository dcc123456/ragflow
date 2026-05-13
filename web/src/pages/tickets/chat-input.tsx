'use client';

import {
  FileUpload,
  FileUploadDropzone,
  FileUploadItem,
  FileUploadItemDelete,
  FileUploadItemMetadata,
  FileUploadItemPreview,
  FileUploadItemProgress,
  FileUploadList,
  FileUploadTrigger,
  type FileUploadProps,
} from '@/components/file-upload';
import { Button } from '@/components/ui/button';
import { Textarea } from '@/components/ui/textarea';
import { formatBytes } from '@/utils/file-util';
import { ArrowUp, Paperclip, Upload, X } from 'lucide-react';
import { useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { toast } from 'sonner';

interface TicketChatInputProps {
  value: string;
  onChange: (value: string) => void;
  files: File[];
  onFilesChange: (files: File[]) => void;
  onSend: () => void;
  sending?: boolean;
  disabled?: boolean;
  placeholder?: string;
  maxFiles?: number;
  maxSize?: number;
}

export function TicketChatInput({
  value,
  onChange,
  files,
  onFilesChange,
  onSend,
  sending = false,
  disabled = false,
  placeholder,
  maxFiles = 5,
  maxSize = 5 * 1024 * 1024,
}: TicketChatInputProps) {
  const { t } = useTranslation();

  const onUpload: NonNullable<FileUploadProps['onUpload']> = useCallback(
    async (uploadedFiles, { onSuccess }) => {
      uploadedFiles.forEach((file) => onSuccess(file));
    },
    [],
  );

  const onFileReject = useCallback((file: File, message: string) => {
    toast(message, {
      description: `"${file.name.length > 20 ? `${file.name.slice(0, 20)}...` : file.name}" has been rejected`,
    });
  }, []);

  const handleSubmit = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    onSend();
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      onSend();
    }
  };

  const sendDisabled =
    (!value.trim() && files.length === 0) || sending || disabled;

  return (
    <FileUpload
      value={files}
      onValueChange={onFilesChange}
      onUpload={onUpload}
      onFileReject={onFileReject}
      maxFiles={maxFiles}
      maxSize={maxSize}
      className="relative w-full items-center"
      multiple
      disabled={disabled || sending}
    >
      <FileUploadDropzone
        tabIndex={-1}
        onClick={(event) => event.preventDefault()}
        className="absolute top-0 left-0 z-0 flex size-full items-center justify-center rounded-none border-none bg-background/50 p-0 opacity-0 backdrop-blur transition-opacity duration-200 ease-out data-[dragging]:z-10 data-[dragging]:opacity-100"
      >
        <div className="flex flex-col items-center gap-1 text-center">
          <div className="flex items-center justify-center rounded-full border p-2.5">
            <Upload className="size-6 text-muted-foreground" />
          </div>
          <p className="font-medium text-sm">
            {t('tickets.detail.dragDropHint')}
          </p>
          <p className="text-muted-foreground text-xs">
            {t('tickets.detail.uploadLimit', {
              count: maxFiles,
              size: formatBytes(maxSize),
            })}
          </p>
        </div>
      </FileUploadDropzone>

      <form
        onSubmit={handleSubmit}
        className="relative flex w-full flex-col gap-2.5 rounded-md border-0.5 border-border-default bg-bg-card p-2 outline-none has-[textarea:focus]:outline-accent-primary has-[textarea:focus]:outline-1 has-[textarea:focus]:outline-offset-2"
      >
        <FileUploadList
          orientation="horizontal"
          className="overflow-x-auto px-0 py-1"
        >
          {files.map((file, index) => (
            <FileUploadItem key={index} value={file} className="max-w-52 p-1.5">
              <FileUploadItemPreview className="size-8 [&>svg]:size-5">
                <FileUploadItemProgress variant="fill" />
              </FileUploadItemPreview>
              <FileUploadItemMetadata size="sm" />
              <FileUploadItemDelete asChild>
                <Button
                  type="button"
                  variant="secondary"
                  size="icon"
                  className="absolute -top-1 -right-1 size-4 shrink-0 cursor-pointer rounded-full"
                >
                  <X className="size-2.5" />
                </Button>
              </FileUploadItemDelete>
            </FileUploadItem>
          ))}
        </FileUploadList>

        <Textarea
          data-testid="ticket-chat-textarea"
          value={value}
          onChange={(e) => onChange(e.target.value)}
          onKeyDown={handleKeyDown}
          placeholder={placeholder}
          className="min-h-10 max-h-40 w-full p-0 overflow-auto !outline-none !border-transparent !bg-transparent !shadow-none !ring-transparent !ring-offset-transparent"
          disabled={disabled || sending}
          autoSize={{ minRows: 2, maxRows: 6 }}
        />

        <div className="flex items-center justify-between gap-2">
          <FileUploadTrigger asChild>
            <Button
              type="button"
              size="icon-xs"
              variant="transparent"
              className="rounded-sm border-0"
              disabled={disabled || sending}
              data-testid="ticket-chat-attach"
            >
              <Paperclip className="size-3.5" />
              <span className="sr-only">Attach file</span>
            </Button>
          </FileUploadTrigger>

          <Button
            type="submit"
            size="icon-xs"
            disabled={sendDisabled}
            loading={sending}
            data-testid="ticket-chat-send"
          >
            <ArrowUp className="size-3.5" />
            <span className="sr-only">Send reply</span>
          </Button>
        </div>
      </form>
    </FileUpload>
  );
}
