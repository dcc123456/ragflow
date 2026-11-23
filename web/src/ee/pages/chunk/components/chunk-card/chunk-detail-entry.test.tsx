/*
 *  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
 *
 *  Licensed under the Apache License, Version 2.0 (the "License");
 *  you may not use this file except in compliance with the License.
 *  You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 *  Unless required by applicable law or agreed to in writing, software
 *  distributed under the License is distributed on an "AS IS" BASIS,
 *  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 *  See the License for the specific language governing permissions and
 *  limitations under the License.
 */

import { formatDate } from '@/utils/date';
import { fireEvent, render, screen } from '@testing-library/react';
import { ChunkDetailEntry } from './chunk-detail-entry';

// The count is the only interpolated key here, so the stub resolves it and
// leaves every other key as its name.
jest.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, options?: { count?: number }) =>
      options?.count === undefined ? key : `${key}: ${options.count}`,
  }),
}));

// `any` on purpose: this file goes through esbuild-jest's babel pass, which
// refuses to strip an imported binding that is also used in a type annotation.
const versionsState: { current: any[] } = { current: [] };

jest.mock('@/ee/hooks/use-chunk-version', () => ({
  __esModule: true,
  useFetchChunkVersions: () => ({
    versions: versionsState.current,
    loading: false,
  }),
}));

const chunk = {
  available_int: 1,
  chunk_id: 'chunk-1',
  content_version: 2,
  content_with_weight: 'Chunk content',
  doc_id: 'doc-1',
  doc_name: 'document.pdf',
  doc_type_kwd: 'text' as const,
  image_id: '',
  positions: [],
};

// Newest first, the order the server answers with.
const versions = [
  {
    version: 2,
    created_by: '',
    restored_from: 0,
    create_time: Date.parse('2026-11-20T12:30:00Z'),
    latest: true,
    kind: 'edit',
  },
  {
    version: 1,
    created_by: '',
    restored_from: 0,
    create_time: Date.parse('2026-11-19T09:00:00Z'),
    latest: false,
    kind: 'edit',
  },
  {
    version: 0,
    created_by: '',
    restored_from: 0,
    create_time: Date.parse('2026-11-18T08:00:00Z'),
    latest: false,
    kind: 'parse',
  },
];

beforeEach(() => {
  versionsState.current = versions;
});

describe('ChunkDetailEntry', () => {
  it('keeps the panel closed until the entry is opened', () => {
    render(<ChunkDetailEntry chunk={chunk} />);

    expect(screen.getByText('chunk.detail')).toBeInTheDocument();
    // The id sits beside the entry, so the card names the chunk before anything
    // is opened; the panel has not rendered yet.
    expect(screen.getByText('chunk-1')).toBeInTheDocument();
    expect(screen.queryByText('chunk.detailTitle')).not.toBeInTheDocument();
  });

  it('shows the id, both instants and the version once opened', () => {
    render(<ChunkDetailEntry chunk={chunk} />);

    fireEvent.click(screen.getByText('chunk.detail'));

    expect(screen.getByText('chunk.detailTitle')).toBeInTheDocument();
    // The corner name and the panel's own id row: both are the chunk.
    expect(screen.getAllByText('chunk-1')).toHaveLength(2);
    // Created is the oldest row, updated the newest one.
    expect(
      screen.getByText(formatDate(versions[2].create_time)),
    ).toBeInTheDocument();
    expect(
      screen.getByText(formatDate(versions[0].create_time)),
    ).toBeInTheDocument();
    // The number alone: the label says it is the version, and the size of the
    // history is not part of it.
    expect(screen.getByText('2')).toBeInTheDocument();
    expect(screen.queryByText(/chunk.versionTotal/)).not.toBeInTheDocument();
    // The type is on the card's own row, so the details do not repeat it.
    expect(screen.queryByText('chunk.docType.text')).not.toBeInTheDocument();
    expect(screen.getByText('chunk.enabled')).toBeInTheDocument();
  });

  it('leaves both instants empty for a chunk with no history', () => {
    versionsState.current = [];

    render(<ChunkDetailEntry chunk={chunk} />);
    fireEvent.click(screen.getByText('chunk.detail'));

    // An unknown instant reads as a dash instead of the epoch; the version stays
    // the one the row the card holds carries, with no count beside it.
    expect(screen.getAllByText('-')).toHaveLength(2);
    expect(screen.getByText('2')).toBeInTheDocument();
    expect(screen.queryByText(/chunk.versionTotal/)).not.toBeInTheDocument();
  });
});
