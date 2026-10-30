import { ChangeEventHandler } from 'react';

/**
 * Enterprise-only contract for the files page tab wrapper. These concepts do
 * not exist upstream — upstream's `pages/files/index.tsx` is a plain
 * file-management page — so they live on the ee side only, in this leaf
 * module (no imports beyond react types) to keep the dependency direction
 * acyclic between the wrapper, the file manager, and navigation hooks.
 */
export enum FileTabs {
  FILE = 'file',
  EVALUATION = 'evaluation',
}

export type FilesInstanceType = {
  searchString?: string;
  onSearchChange?: ChangeEventHandler<HTMLInputElement>;
  showFileUploadModal?: () => void;
  showSyncFileModal?: () => void;
  showFolderCreateModal?: () => void;
};
