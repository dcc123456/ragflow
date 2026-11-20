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

import { useFetchDatasetVersions } from '@/ee/hooks/use-dataset-version';
import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from '@testing-library/react';
import { toast } from '@/components/hooks/use-toast';
import DatasetVersionHistory from '.';

// The restore answer is the only place the documents it could not fill are named,
// so the toast is what carries that news: swap it for a spy.
jest.mock('@/components/hooks/use-toast', () => ({
  __esModule: true,
  toast: jest.fn(),
}));

const mockToast = toast as unknown as jest.Mock;

jest.mock('react-i18next', () => {
  // The real module is kept so the app wide i18n instance still initialises
  // when the component's import chain pulls in `@/locales/config`.
  const actual = jest.requireActual('react-i18next');
  return {
    ...actual,
    useTranslation: () => ({
      t: (
        key: string,
        options?: { user?: string; chunk?: string; document?: string },
      ) => {
        if (options?.user !== undefined) {
          return `${key}: ${options.user}`;
        }
        // A chunk sentence names the chunk and its document, which is what the
        // tests assert on, so only it interpolates; every other line keeps its
        // bare key and the assertions that name it stay exact.
        if (key === 'datasetVersion.chunkChangeLine') {
          return `${key}: ${options?.chunk} of ${options?.document}`;
        }
        return key;
      },
    }),
  };
});

// `any` on purpose: this file goes through esbuild-jest's babel pass, which
// refuses to strip an imported binding that is also used in a type annotation.
const mockVersions: { current: any[] } = { current: [] };
const mockDetail: { current: any } = { current: undefined };
// The head the list was read at, and the one a re-read right before a write
// returns: they differ whenever the dataset moved on while the panel was open.
const mockListHead: { current: number } = { current: 3 };
const mockFreshHead: { current: number } = { current: 3 };
const mockRestore = jest.fn();
// The dry run a restore starts with: it answers whether the dataset can be
// restored right now (a document may be queued or parsing).
const mockCheckRestore = jest.fn();

jest.mock('@/ee/hooks/use-dataset-version', () => ({
  __esModule: true,
  useFetchDatasetVersions: jest.fn(() => ({
    versions: mockVersions.current,
    currentVersion: mockListHead.current,
    loading: false,
    refetch: () =>
      Promise.resolve({
        data: {
          code: 0,
          data: { current_version: mockFreshHead.current },
        },
      }),
  })),
  useFetchDatasetVersion: () => ({
    detail: mockDetail.current,
    loading: false,
    missingVersion: false,
  }),
  useRestoreDatasetVersion: () => ({
    restoreDatasetVersion: mockRestore,
    loading: false,
  }),
  useCheckDatasetVersionRestore: () => ({
    checkRestore: mockCheckRestore,
    loading: false,
  }),
  useDeleteDatasetVersion: () => ({
    deleteDatasetVersion: jest.fn(),
    loading: false,
  }),
}));

// A restore shows the dry run's plan first and writes only when that dialog is
// confirmed: every kind of version restores documents, so the click alone must not
// start the write.
const confirmRestore = async () => {
  fireEvent.click(
    await screen.findByRole('button', { name: 'common.confirm' }),
  );
};

describe('dataset version history list', () => {
  beforeEach(() => {
    mockVersions.current = [];
    mockDetail.current = undefined;
    mockListHead.current = 3;
    mockFreshHead.current = 3;
    mockRestore.mockReset();
    mockRestore.mockResolvedValue({ code: 0, data: {} });
    mockCheckRestore.mockReset();
    // An idle dataset: the dry run reports no reason to refuse.
    mockCheckRestore.mockResolvedValue({ code: 0, data: {} });
    (useFetchDatasetVersions as unknown as jest.Mock).mockClear();
    mockToast.mockClear();
  });

  it('labels a renamed document as a version of its own', () => {
    mockVersions.current = [
      {
        version: 2,
        kind: 'document_rename',
        created_by: 'user-1',
        create_time: 1762764000000,
      },
    ];

    render(<DatasetVersionHistory datasetId="ds-1" />);

    // A rename is not a settings change, so it must not read as one.
    expect(
      screen.getByText('datasetVersion.kindDocumentRename'),
    ).toBeInTheDocument();
    expect(
      screen.queryByText('datasetVersion.kindDocumentConfig'),
    ).not.toBeInTheDocument();
  });

  it('labels a document switch as a version of the document', () => {
    mockVersions.current = [
      {
        version: 3,
        kind: 'document_status',
        created_by: 'user-1',
        create_time: 1762850500000,
      },
    ];

    render(<DatasetVersionHistory datasetId="ds-1" />);

    // The chunks follow the document's switch, so the version reads as a
    // document change rather than a chunk edit.
    expect(
      screen.getByText('datasetVersion.kindDocumentStatus'),
    ).toBeInTheDocument();
    expect(
      screen.queryByText('datasetVersion.kindChunkUpdate'),
    ).not.toBeInTheDocument();
  });

  it('names both sides of a rename in its change line', () => {
    mockVersions.current = [
      {
        version: 5,
        kind: 'document_rename',
        created_by: 'user-1',
        create_time: 1762850500000,
      },
    ];
    mockDetail.current = {
      version: 5,
      kind: 'document_rename',
      documents: [],
      changes: [
        {
          entity: 'document',
          action: 'rename',
          doc_id: 'doc-1',
          name: '三国人物2222.txt',
          previous_name: '三国人物.txt',
        },
      ],
      config: {},
    };

    render(<DatasetVersionHistory datasetId="ds-1" />);

    // The name a rename landed on says nothing about what moved, so both sides
    // are named; the one-sided sentence is what an older row still reads.
    expect(
      screen.getByText('datasetVersion.changesRenameLine'),
    ).toBeInTheDocument();
    expect(
      screen.queryByText('datasetVersion.changesLine'),
    ).not.toBeInTheDocument();
  });

  it('names the actor of every version in its own column', () => {
    mockVersions.current = [
      {
        version: 3,
        kind: 'dataset_config',
        created_by: 'user-3',
        created_by_name: 'Alice',
        create_time: 1762764000000,
      },
      {
        // A version whose user row no longer exists: the server leaves the name
        // empty, so the raw id is the only thing left to show.
        version: 2,
        kind: 'chunk',
        created_by: 'deleted-user-id',
        create_time: 1762677600000,
      },
      {
        // No actor recorded at all: an em dash keeps the column rhythm.
        version: 1,
        kind: 'upload',
        created_by: '',
        create_time: 1762591200000,
      },
    ];

    render(<DatasetVersionHistory datasetId="ds-1" />);

    expect(
      screen.getByText('datasetVersion.columnCreatedBy'),
    ).toBeInTheDocument();
    expect(screen.getByText('Alice')).toBeInTheDocument();
    expect(screen.getByText('deleted-user-id')).toBeInTheDocument();
    expect(screen.getByText('—')).toBeInTheDocument();
  });

  it('lists the configuration fields a configuration version moved', () => {
    mockVersions.current = [
      {
        version: 1,
        kind: 'dataset_config',
        created_by: 'user-1',
        created_by_name: 'Alice',
        create_time: 1762591200000,
      },
    ];
    mockDetail.current = {
      version: 1,
      kind: 'dataset_config',
      documents: [],
      changes: [],
      config: { name: 'Dataset', similarity_threshold: 0.3 },
      config_changes: [
        { field: 'similarity_threshold', before: 0.2, after: 0.3 },
        // A parser parameter arrives as a path: the section holds the label, the
        // rest is the parameter's own name.
        { field: 'parser_config.chunk_token_num', before: 512, after: 1024 },
        // An avatar is a base64 data URI and must not be printed.
        { field: 'avatar', before: '', after: 'data:image/png;base64,AAAA' },
      ],
    };

    render(<DatasetVersionHistory datasetId="ds-1" />);

    // A configuration version records no document or chunk rows, so the diff is
    // its change list: a head line, then one line per field under it.
    expect(screen.getByText('datasetVersion.changesTitle')).toBeInTheDocument();
    expect(
      screen.getByText('datasetVersion.changesConfiguration'),
    ).toBeInTheDocument();
    expect(screen.getAllByText('datasetVersion.configChangeLine')).toHaveLength(
      3,
    );
    expect(screen.queryByText(/base64/)).not.toBeInTheDocument();
  });

  it('names the parse settings a document-configuration version moved', () => {
    mockVersions.current = [
      {
        version: 2,
        kind: 'document_config',
        created_by: 'user-1',
        create_time: 1762764000000,
      },
    ];
    mockDetail.current = {
      version: 2,
      kind: 'document_config',
      documents: [],
      changes: [
        {
          entity: 'document',
          action: 'update',
          doc_id: 'doc-1',
          name: 'handbook.pdf',
        },
        {
          entity: 'document',
          action: 'update',
          doc_id: 'doc-2',
          name: 'manual.pdf',
        },
        // Its settings history has no earlier row, so the server reports no
        // group for it and the plain sentence is what is left.
        {
          entity: 'document',
          action: 'update',
          doc_id: 'doc-3',
          name: 'notes.pdf',
        },
      ],
      config: {},
      document_config_changes: [
        {
          doc_id: 'doc-1',
          doc_name: 'handbook.pdf',
          changes: [
            { field: 'parser_id', before: 'naive', after: 'paper' },
            {
              field: 'parser_config.chunk_token_num',
              before: 512,
              after: 1024,
            },
          ],
        },
        {
          doc_id: 'doc-2',
          doc_name: 'manual.pdf',
          changes: [
            { field: 'parser_config.delimiter', before: '\n', after: '##' },
          ],
        },
      ],
    };

    render(<DatasetVersionHistory datasetId="ds-1" />);

    // One head line naming the documents whose settings moved, then one line per
    // distinct setting. Those documents' own sentences are skipped - the head
    // line names them - while the document whose settings could not be diffed
    // keeps its sentence, since nothing else would mention it.
    expect(
      screen.getByText('datasetVersion.changesDocumentConfigurationFor'),
    ).toBeInTheDocument();
    expect(screen.getAllByText('datasetVersion.configChangeLine')).toHaveLength(
      3,
    );
    expect(screen.getAllByText('datasetVersion.changesLine')).toHaveLength(1);
    // The version did record a change, so the empty-state line stays away.
    expect(
      screen.queryByText('datasetVersion.changesEmpty'),
    ).not.toBeInTheDocument();
  });

  it('names no document when the server could not resolve any', () => {
    // A document the server cannot name reports only its id; printing that raw
    // id under "for" would read worse than naming no document at all, and the
    // settings below still say what moved.
    mockVersions.current = [];
    mockDetail.current = {
      version: 4,
      kind: 'document_config',
      changes: [
        {
          entity: 'document',
          action: 'update',
          doc_id: 'doc-1',
          name: '',
        },
      ],
      config: {},
      document_config_changes: [
        {
          doc_id: 'doc-1',
          changes: [{ field: 'parser_id', before: 'naive', after: 'paper' }],
        },
      ],
    };

    render(<DatasetVersionHistory datasetId="ds-1" />);

    expect(
      screen.getByText('datasetVersion.changesDocumentConfiguration'),
    ).toBeInTheDocument();
    expect(
      screen.queryByText('datasetVersion.changesDocumentConfigurationFor'),
    ).not.toBeInTheDocument();
    expect(screen.getAllByText('datasetVersion.configChangeLine')).toHaveLength(
      1,
    );
  });

  it('lists a wiki page edit as a version of its own', () => {
    mockVersions.current = [
      {
        version: 6,
        kind: 'artifact_edit',
        created_by: 'user-1',
        create_time: 1762850400000,
      },
    ];
    mockDetail.current = {
      version: 6,
      kind: 'artifact_edit',
      documents: [],
      changes: [
        {
          entity: 'artifact',
          action: 'update',
          doc_id: 'ds-1/wiki/本主',
          name: '本主',
        },
      ],
      config: {},
    };

    render(<DatasetVersionHistory datasetId="ds-1" />);

    // The page edit is labelled like every other kind, and its change entry is
    // still a document-shaped sentence so the list renders it the same way.
    expect(
      screen.getAllByText('datasetVersion.kindArtifactEdit').length,
    ).toBeGreaterThan(0);
    expect(screen.getByText('datasetVersion.changesLine')).toBeInTheDocument();
  });

  it('lists a wiki page edit without replaying its diff', () => {
    mockVersions.current = [
      {
        version: 7,
        kind: 'artifact_edit',
        created_by: 'user-1',
        create_time: 1762850500000,
      },
    ];
    mockDetail.current = {
      version: 7,
      kind: 'artifact_edit',
      documents: [],
      changes: [
        {
          entity: 'artifact',
          action: 'update',
          doc_id: 'ds-1/wiki/本主',
          name: '本主',
          commit_id: 'commit-1',
        },
      ],
      config: {},
    };

    render(<DatasetVersionHistory datasetId="ds-1" />);

    // The sentence names the page, and the entry stops there: neither the change
    // line's own title nor any diff line follows it.
    expect(screen.getByText('datasetVersion.changesLine')).toBeInTheDocument();
    expect(
      screen.queryByText('datasetVersion.artifactDiffTitle'),
    ).not.toBeInTheDocument();
    expect(screen.queryByText('+地方保护神')).not.toBeInTheDocument();
    expect(screen.queryByText('-地方神祇')).not.toBeInTheDocument();
  });

  it('lists a chunk edit without replaying its diff', () => {
    mockVersions.current = [
      {
        version: 9,
        kind: 'chunk',
        created_by: 'user-1',
        create_time: 1762850700000,
      },
    ];
    mockDetail.current = {
      version: 9,
      kind: 'chunk',
      documents: [],
      changes: [
        {
          entity: 'chunk',
          action: 'update',
          doc_id: 'doc-1',
          chunk_id: 'chunk-1',
          doc_name: '三国人物2.txt',
          chunk_version: 3,
          name: '曹操，字孟德。',
        },
      ],
      config: {},
    };

    render(<DatasetVersionHistory datasetId="ds-1" />);

    // The change line is the whole entry: it names the chunk and its document, and
    // the text the chunk held stays in the chunk's own history instead of being
    // pasted under every edit.
    expect(
      screen.getByText(
        'datasetVersion.chunkChangeLine: chunk-1 of 三国人物2.txt',
      ),
    ).toBeInTheDocument();
    // The entry names the version it produced, so the row can be opened next to
    // the change that made it.
    expect(
      screen.getByText('datasetVersion.chunkVersionTag'),
    ).toBeInTheDocument();
    expect(screen.queryByText('曹操，字孟德。')).not.toBeInTheDocument();
    expect(
      screen.queryByText('datasetVersion.chunkDiffTitle'),
    ).not.toBeInTheDocument();
    expect(screen.queryByText('+曹操，字孟德。')).not.toBeInTheDocument();
    expect(screen.queryByText('-曹操，字孟德')).not.toBeInTheDocument();
    expect(
      screen.queryByText('datasetVersion.currentChunk'),
    ).not.toBeInTheDocument();
  });

  it('lists every removed chunk as its own sentence', () => {
    mockVersions.current = [
      {
        version: 9,
        kind: 'chunk',
        created_by: 'user-1',
        create_time: 1762850700000,
      },
    ];
    mockDetail.current = {
      version: 9,
      kind: 'chunk',
      documents: [],
      changes: [
        {
          entity: 'chunk',
          action: 'delete',
          doc_id: 'doc-1',
          chunk_id: 'chunk-1',
          doc_name: '三国人物2.txt',
        },
        {
          entity: 'chunk',
          action: 'delete',
          doc_id: 'doc-1',
          chunk_id: 'chunk-2',
          doc_name: '三国人物2.txt',
        },
        {
          entity: 'chunk',
          action: 'update',
          doc_id: 'doc-1',
          chunk_id: 'chunk-3',
          doc_name: '三国人物2.txt',
          name: '曹操，字孟德。',
        },
      ],
      config: {},
    };

    render(<DatasetVersionHistory datasetId="ds-1" />);

    // A removal is one sentence per chunk, read exactly like an edit: which
    // chunk of which document went. Two removals and the edit are three lines.
    expect(
      screen.getAllByText(/^datasetVersion\.chunkChangeLine: chunk-\d/),
    ).toHaveLength(3);
    expect(
      screen.getByText(
        'datasetVersion.chunkChangeLine: chunk-1 of 三国人物2.txt',
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByText(
        'datasetVersion.chunkChangeLine: chunk-2 of 三国人物2.txt',
      ),
    ).toBeInTheDocument();
    // The edit beside them is its own sentence too, and the text it now holds
    // stays in the chunk's history.
    expect(
      screen.getByText(
        'datasetVersion.chunkChangeLine: chunk-3 of 三国人物2.txt',
      ),
    ).toBeInTheDocument();
    expect(screen.queryByText('曹操，字孟德。')).not.toBeInTheDocument();
  });

  it('names the document of a removed chunk, falling back to what it knows', () => {
    mockVersions.current = [
      {
        version: 9,
        kind: 'chunk_delete',
        created_by: 'user-1',
        create_time: 1762850700000,
      },
    ];
    mockDetail.current = {
      version: 9,
      kind: 'chunk_delete',
      documents: [],
      changes: [
        {
          entity: 'chunk',
          action: 'delete',
          doc_id: 'doc-1',
          chunk_id: 'chunk-1',
          doc_name: '三国人物2.txt',
          // The tombstone the removal wrote: what the reader opens the chunk's
          // own history at.
          chunk_version: 5,
        },
        // The document was deleted from the dataset: its name is gone, and the
        // id it still knows is what the line falls back to.
        {
          entity: 'chunk',
          action: 'delete',
          doc_id: 'doc-3',
          chunk_id: 'chunk-3',
        },
        // Neither a name nor an id: the line still names the chunk, and the
        // document slot is the panel's dash.
        {
          entity: 'chunk',
          action: 'delete',
          chunk_id: 'chunk-4',
        },
      ],
      config: {},
    };

    render(<DatasetVersionHistory datasetId="ds-1" />);

    expect(
      screen.getAllByText(/^datasetVersion\.chunkChangeLine: chunk-\d/),
    ).toHaveLength(3);
    expect(
      screen.getByText(
        'datasetVersion.chunkChangeLine: chunk-1 of 三国人物2.txt',
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByText('datasetVersion.chunkChangeLine: chunk-3 of doc-3'),
    ).toBeInTheDocument();
    expect(
      screen.getByText('datasetVersion.chunkChangeLine: chunk-4 of —'),
    ).toBeInTheDocument();
    // The version the removal recorded travels too; a removal whose history was
    // pruned shows its id alone.
    expect(screen.getAllByText('datasetVersion.chunkVersionTag')).toHaveLength(
      1,
    );
  });

  it('gives two chunks of same-named documents a line each', () => {
    mockVersions.current = [
      {
        version: 9,
        kind: 'chunk_delete',
        created_by: 'user-1',
        create_time: 1762850700000,
      },
    ];
    mockDetail.current = {
      version: 9,
      kind: 'chunk_delete',
      documents: [],
      changes: [
        // Two uploads of the same file: the names match, the documents do not.
        // A chunk line names its chunk, so the two removals stay apart.
        {
          entity: 'chunk',
          action: 'delete',
          doc_id: 'doc-1',
          chunk_id: 'chunk-1',
          doc_name: 'handbook.pdf',
        },
        {
          entity: 'chunk',
          action: 'delete',
          doc_id: 'doc-2',
          chunk_id: 'chunk-2',
          doc_name: 'handbook.pdf',
        },
      ],
      config: {},
    };

    render(<DatasetVersionHistory datasetId="ds-1" />);

    expect(
      screen.getByText(
        'datasetVersion.chunkChangeLine: chunk-1 of handbook.pdf',
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByText(
        'datasetVersion.chunkChangeLine: chunk-2 of handbook.pdf',
      ),
    ).toBeInTheDocument();
  });

  it('folds a long change list behind a toggle', () => {
    mockVersions.current = [
      {
        version: 9,
        kind: 'chunk_delete',
        created_by: 'user-1',
        create_time: 1762850700000,
      },
    ];
    mockDetail.current = {
      version: 9,
      kind: 'chunk_delete',
      documents: [],
      // A bulk delete of twelve chunks: the fold keeps the list readable, and the
      // toggle says how many rows are still folded.
      changes: Array.from({ length: 12 }, (_, index) => ({
        entity: 'chunk',
        action: 'delete',
        doc_id: 'doc-1',
        chunk_id: `chunk-${index + 1}`,
        doc_name: 'handbook.pdf',
      })),
      // The writer kept only these twelve of fifteen changes: the fold and the
      // sample note are two different limits, and both are true at once.
      changes_total: 15,
      config: {},
    };

    render(<DatasetVersionHistory datasetId="ds-1" />);

    expect(
      screen.getAllByText(/^datasetVersion\.chunkChangeLine: chunk-\d/),
    ).toHaveLength(10);
    expect(
      screen.getByText('datasetVersion.changesShowAll'),
    ).toBeInTheDocument();
    expect(
      screen.queryByText(/chunkChangeLine: chunk-12/),
    ).not.toBeInTheDocument();
    // The sample note sits beside the fold and speaks about what was recorded,
    // not about what the fold hides: the two counts never read as one number.
    expect(
      screen.getByText('datasetVersion.changesTruncated'),
    ).toBeInTheDocument();

    fireEvent.click(screen.getByText('datasetVersion.changesShowAll'));

    expect(
      screen.getAllByText(/^datasetVersion\.chunkChangeLine: chunk-\d/),
    ).toHaveLength(12);
    expect(
      screen.getByText('datasetVersion.changesShowLess'),
    ).toBeInTheDocument();
    // Expanding the fold cannot reveal what the writer never stored.
    expect(
      screen.getByText('datasetVersion.changesTruncated'),
    ).toBeInTheDocument();
  });

  it('names the metadata keys a metadata version moved', () => {
    mockVersions.current = [
      {
        version: 5,
        kind: 'document_meta',
        created_by: 'user-1',
        create_time: 1762850400000,
      },
    ];
    mockDetail.current = {
      version: 5,
      kind: 'document_meta',
      documents: [],
      changes: [
        {
          entity: 'document',
          action: 'update',
          doc_id: 'doc-1',
          name: 'handbook.pdf',
          meta_changes: [
            // A changed key says what it held and what it holds now.
            { key: 'author', before: 'alice', after: 'bob' },
            // An introduced key has no before: the side is absent, not empty.
            { key: 'tags', after: ['a', 'b'] },
          ],
        },
      ],
      config: {},
    };

    render(<DatasetVersionHistory datasetId="ds-1" />);

    // The sentence names the document and says what moved - the metadata - and
    // the keys follow: "the metadata changed" without the values is not
    // actionable.
    expect(
      screen.getByText('datasetVersion.changesMetadataLine'),
    ).toBeInTheDocument();
    expect(
      screen.queryByText('datasetVersion.changesLine'),
    ).not.toBeInTheDocument();
    expect(screen.getAllByText('datasetVersion.configChangeLine')).toHaveLength(
      2,
    );
  });

  it('says the change list is a sample when the writer cut it', () => {
    mockVersions.current = [
      {
        version: 12,
        kind: 'delete',
        created_by: 'user-1',
        create_time: 1762850600000,
      },
    ];
    mockDetail.current = {
      version: 12,
      kind: 'delete',
      documents: [],
      changes: [
        { entity: 'document', action: 'delete', doc_id: 'doc-1', name: 'a.md' },
        { entity: 'document', action: 'delete', doc_id: 'doc-2', name: 'b.md' },
      ],
      // The writer named 1400 documents and stores a sample of them.
      changes_total: 1400,
      config: {},
    };

    render(<DatasetVersionHistory datasetId="ds-1" />);

    // The line sits under the rows, so the reader learns the list ends early
    // before acting on how short it looks.
    expect(
      screen.getByText('datasetVersion.changesTruncated'),
    ).toBeInTheDocument();
  });

  it('says nothing about a sample when the list is whole', () => {
    mockVersions.current = [
      {
        version: 13,
        kind: 'delete',
        created_by: 'user-1',
        create_time: 1762850700000,
      },
    ];
    mockDetail.current = {
      version: 13,
      kind: 'delete',
      documents: [],
      changes: [
        { entity: 'document', action: 'delete', doc_id: 'doc-1', name: 'a.md' },
        { entity: 'document', action: 'delete', doc_id: 'doc-2', name: 'b.md' },
      ],
      changes_total: 2,
      config: {},
    };

    render(<DatasetVersionHistory datasetId="ds-1" />);

    expect(
      screen.queryByText('datasetVersion.changesTruncated'),
    ).not.toBeInTheDocument();
  });

  it('opens the detail panel with a summary, then its changes', () => {
    mockVersions.current = [
      {
        version: 3,
        kind: 'chunk',
        created_by: 'user-3',
        created_by_name: 'Alice',
        create_time: 1762764000000,
      },
    ];
    mockDetail.current = {
      version: 3,
      kind: 'chunk',
      documents: [
        { id: 'doc-1', name: '三国人物2.txt' },
        { id: 'doc-2', name: '三国人物1.txt' },
      ],
      changes: [
        { entity: 'document', action: 'add', name: '三国人物2.txt' },
        {
          entity: 'chunk',
          action: 'update',
          doc_id: 'doc-1',
          chunk_id: 'chunk-1',
          doc_name: '三国人物2.txt',
          name: '曹操，字孟德',
        },
      ],
      config: {},
    };

    render(<DatasetVersionHistory datasetId="ds-1" />);

    expect(screen.getByText('datasetVersion.summaryTitle')).toBeInTheDocument();
    expect(screen.getByText('datasetVersion.counts')).toBeInTheDocument();
    expect(screen.getByText('datasetVersion.changesTitle')).toBeInTheDocument();
    // The document set comes before the change set, named rather than counted:
    // no chunk count is reported per version.
    expect(
      screen.getByText('datasetVersion.documentsTitle'),
    ).toBeInTheDocument();
    expect(screen.getByText('三国人物2.txt')).toBeInTheDocument();
    expect(
      screen.queryByText('datasetVersion.documentChunks'),
    ).not.toBeInTheDocument();
    // One sentence per change, not an entity badge beside an action column. A
    // document entry says what happened to which file...
    expect(screen.getByText('datasetVersion.changesLine')).toBeInTheDocument();
    // ...while a chunk entry names the chunk and its document and stops there: the
    // text it held at this version is the chunk's own history, not part of the
    // change list.
    expect(
      screen.getByText(
        'datasetVersion.chunkChangeLine: chunk-1 of 三国人物2.txt',
      ),
    ).toBeInTheDocument();
    expect(
      screen.queryByText('datasetVersion.currentChunk'),
    ).not.toBeInTheDocument();
    expect(screen.queryByText('曹操，字孟德')).not.toBeInTheDocument();
    // The head marker qualifies the summary, so it sits on that block; a content
    // version has nothing else that could carry it.
    expect(screen.getByText('datasetVersion.current')).toBeInTheDocument();
    // Who made the change is the list's column now; the panel does not repeat it.
    expect(screen.queryByText(/changedBy/)).not.toBeInTheDocument();
    // The panel no longer explains a restore in a sentence of its own. The only
    // case that needs saying - the selected row is already the head, so the
    // restore button is disabled for a reason - says it on the button.
    expect(
      screen.queryByText('datasetVersion.restoreCurrentTip'),
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole('button', { name: 'datasetVersion.restore' }),
    ).toHaveAttribute('title', 'datasetVersion.restoreCurrentTip');
  });

  it('says the configuration changed, without a snapshot copy', () => {
    mockVersions.current = [
      {
        version: 3,
        kind: 'dataset_config',
        created_by: 'user-3',
        create_time: 1762764000000,
      },
    ];
    mockDetail.current = {
      version: 3,
      kind: 'dataset_config',
      changes: [],
      config: { name: 'Dataset' },
    };

    render(<DatasetVersionHistory datasetId="ds-1" />);

    // The panel does not repeat the snapshot: no heading, and no field rows
    // either - the change section is the whole answer.
    expect(
      screen.queryByText('datasetVersion.configurationTitle'),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByText('datasetVersion.field.name'),
    ).not.toBeInTheDocument();
    // A configuration version records no document or chunk rows; the change
    // section stays and says what did move, rather than vanishing.
    expect(screen.getByText('datasetVersion.changesTitle')).toBeInTheDocument();
    expect(
      screen.getByText('datasetVersion.changesConfiguration'),
    ).toBeInTheDocument();
    // The empty-state line belongs to content versions, which do record a set.
    expect(
      screen.queryByText('datasetVersion.changesEmpty'),
    ).not.toBeInTheDocument();
    // No note about restoring is repeated here: the panel states what changed,
    // and the restore button speaks for itself.
    expect(
      screen.queryByText('datasetVersion.restoreCurrentTip'),
    ).not.toBeInTheDocument();
  });

  it('files a restore under the change set, not under the summary', () => {
    // No list rows: the only place the source version could come from is the
    // panel itself, so the query says where the line landed.
    mockVersions.current = [];
    mockDetail.current = {
      version: 3,
      kind: 'restore',
      restored_from: 1,
      changes: [],
      config: { name: 'Dataset' },
    };

    render(<DatasetVersionHistory datasetId="ds-1" />);

    const summary = screen
      .getByText('datasetVersion.summaryTitle')
      .closest('section');
    const changes = screen
      .getByText('datasetVersion.changesTitle')
      .closest('section');

    expect(summary).not.toBeNull();
    expect(changes).not.toBeNull();
    expect(
      within(changes as HTMLElement).getByText('datasetVersion.restoredFrom'),
    ).toBeInTheDocument();
    // The summary holds counts; the restore is what the version did, so it is a
    // change, and it does not repeat above the change set.
    expect(
      within(summary as HTMLElement).queryByText('datasetVersion.restoredFrom'),
    ).not.toBeInTheDocument();
  });

  it('shows a restore as the version it came from, and nothing else', () => {
    // A restore's change is the state it put back, so the source version says
    // the whole of it. The rows underneath would only restate what that
    // version's own history records, one line above the document set this
    // version holds.
    mockVersions.current = [];
    mockDetail.current = {
      version: 3,
      kind: 'restore',
      restored_from: 2,
      documents: [],
      changes: [
        {
          entity: 'document',
          action: 'delete',
          doc_id: 'doc-9',
          name: 'test_excel_1.xlsx',
        },
      ],
    };

    render(<DatasetVersionHistory datasetId="ds-1" />);

    const changes = screen
      .getByText('datasetVersion.changesTitle')
      .closest('section') as HTMLElement;
    expect(
      within(changes).getByText('datasetVersion.restoredFrom'),
    ).toBeInTheDocument();
    // No change rows and no outcome counts: one line says the whole change.
    expect(
      within(changes).queryByText('datasetVersion.changesLine'),
    ).not.toBeInTheDocument();
    expect(within(changes).queryAllByRole('listitem')).toHaveLength(0);
  });

  it('warns about the documents a restore could not fill', async () => {
    mockVersions.current = [
      {
        version: 2,
        kind: 'dataset_config',
        created_by: 'user-1',
        create_time: 1762764000000,
      },
    ];
    mockRestore.mockResolvedValue({
      code: 0,
      data: {
        version: 3,
        pre_version: 2,
        applied: true,
        changes: [],
        restored_documents: ['doc-1'],
        deleted_documents: [],
        missing_documents: ['doc-9', 'doc-10'],
        missing_document_reasons: {
          'doc-9': 'replay_failed',
          'doc-10': 'no_chunk_history',
        },
      },
    });
    mockDetail.current = {
      version: 3,
      kind: 'restore',
      restored_from: 2,
      documents: [],
      changes: [],
    };

    render(<DatasetVersionHistory datasetId="ds-1" />);

    fireEvent.click(
      screen.getByRole('button', { name: 'datasetVersion.restore' }),
    );
    await confirmRestore();

    await waitFor(() => expect(mockToast).toHaveBeenCalledTimes(1));
    const [notice] = mockToast.mock.calls[0];
    expect(notice.title).toBe('datasetVersion.restoreIncompleteTitle');
    // The server said why, so the reader gets the reasons beside the ids rather
    // than a bare list.
    expect(notice.description).toBe(
      'datasetVersion.restoreIncompleteBodyWithReasons',
    );
  });

  it('stays quiet when the restore filled every document', async () => {
    mockVersions.current = [
      {
        version: 2,
        kind: 'dataset_config',
        created_by: 'user-1',
        create_time: 1762764000000,
      },
    ];
    mockRestore.mockResolvedValue({
      code: 0,
      data: {
        version: 3,
        pre_version: 2,
        applied: true,
        changes: [],
        restored_documents: ['doc-1'],
        deleted_documents: [],
      },
    });
    mockDetail.current = {
      version: 3,
      kind: 'restore',
      restored_from: 2,
      documents: [],
      changes: [],
    };

    render(<DatasetVersionHistory datasetId="ds-1" />);

    fireEvent.click(
      screen.getByRole('button', { name: 'datasetVersion.restore' }),
    );
    await confirmRestore();

    await waitFor(() => expect(mockRestore).toHaveBeenCalled());
    expect(mockToast).not.toHaveBeenCalled();
  });

  it('lists a restore version s documents without a content warning', () => {
    // A version taken before a document was parsed held no chunk for it, which is
    // the state that version describes rather than a restore that lost content:
    // the document list names the files and says nothing about their chunks.
    mockVersions.current = [];
    mockDetail.current = {
      version: 3,
      kind: 'restore',
      restored_from: 2,
      changes: [],
      documents: [
        {
          id: 'doc-empty',
          name: 'empty.pdf',
          chunk_num: 0,
          restored_version: 2,
        },
        {
          id: 'doc-filled',
          name: 'filled.pdf',
          chunk_num: 12,
          restored_version: 2,
        },
        { id: 'doc-gone', name: 'gone.pdf' },
      ],
    };

    render(<DatasetVersionHistory datasetId="ds-1" />);

    expect(screen.getByText('empty.pdf')).toBeInTheDocument();
    expect(screen.getByText('filled.pdf')).toBeInTheDocument();
    expect(screen.getByText('gone.pdf')).toBeInTheDocument();
    expect(
      screen.queryByText('datasetVersion.documentsIncomplete'),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByText('datasetVersion.documentNeedsReparse'),
    ).not.toBeInTheDocument();
  });

  it('aims the actions at the row the list has highlighted', async () => {
    mockVersions.current = [
      {
        version: 3,
        kind: 'chunk',
        created_by: 'user-1',
        create_time: 1762764000000,
      },
      {
        version: 2,
        kind: 'chunk',
        created_by: 'user-1',
        create_time: 1762763000000,
      },
    ];
    mockDetail.current = {
      version: 3,
      kind: 'chunk',
      changes: [],
      config: {},
    };

    render(<DatasetVersionHistory datasetId="ds-1" />);

    // The list opens on the head, which cannot be restored onto itself.
    const restore = screen.getByRole('button', {
      name: 'datasetVersion.restore',
    });
    expect(restore).toBeInTheDocument();
    expect(restore).toBeDisabled();

    // Picking a row is what the actions follow.
    const rows = screen.getAllByRole('row');
    fireEvent.click(rows[rows.length - 1]);
    await waitFor(() => expect(restore).toBeEnabled());

    fireEvent.click(restore);
    // Nothing is written until the plan is confirmed.
    expect(mockRestore).not.toHaveBeenCalled();
    await confirmRestore();
    await waitFor(() =>
      expect(mockRestore).toHaveBeenCalledWith(
        expect.objectContaining({ version: 2 }),
      ),
    );
  });

  it('pins a restore to the head it re-reads, not the one the list was read at', async () => {
    // The list was read when the head was 3; by the time the user restores, an
    // upload elsewhere has appended versions up to 17. Restoring pinned to 3
    // would be refused with a 409, so the panel reads the head again first.
    mockListHead.current = 3;
    mockFreshHead.current = 17;
    mockVersions.current = [
      {
        version: 2,
        kind: 'dataset_config',
        created_by: 'user-1',
        create_time: 1762764000000,
      },
    ];

    render(<DatasetVersionHistory datasetId="ds-1" />);

    fireEvent.click(
      screen.getByRole('button', { name: 'datasetVersion.restore' }),
    );
    await confirmRestore();

    await waitFor(() =>
      expect(mockRestore).toHaveBeenCalledWith({
        dataset_id: 'ds-1',
        version: 2,
        expected_version: 17,
      }),
    );
  });

  it('refuses a restore while a document is being parsed', async () => {
    // A parse writes the chunks a restore rebuilds, so the dry run answers with
    // a reason instead of a plan and the panel shows it before anything is
    // written.
    mockVersions.current = [
      {
        version: 2,
        kind: 'dataset_config',
        created_by: 'user-1',
        create_time: 1762764000000,
      },
    ];
    mockCheckRestore.mockResolvedValue({
      code: 0,
      data: {
        blocked_reason:
          'cannot restore while a document is being parsed: handbook.pdf; stop it and wait for a terminal state',
      },
    });

    render(<DatasetVersionHistory datasetId="ds-1" />);

    fireEvent.click(
      screen.getByRole('button', { name: 'datasetVersion.restore' }),
    );

    await waitFor(() =>
      expect(
        screen.getByText('datasetVersion.restoreBlockedTitle'),
      ).toBeInTheDocument(),
    );
    expect(screen.getByText(/handbook\.pdf/)).toBeInTheDocument();
    expect(mockRestore).not.toHaveBeenCalled();
  });

  it.each([
    [
      { restored_documents: ['doc-1', 'doc-2'], deleted_documents: [] },
      'datasetVersion.restoreConfirmRestoreOnly',
    ],
    [
      { restored_documents: [], deleted_documents: ['doc-1', 'doc-2'] },
      'datasetVersion.restoreConfirmDeleteOnly',
    ],
    [
      { restored_documents: ['doc-1'], deleted_documents: ['doc-2'] },
      'datasetVersion.restoreConfirmBoth',
    ],
  ])(
    'names only the direction the restore actually moves documents in',
    async (plan, key) => {
      // Rolling back an upload restores nothing, and a version whose documents
      // are all still there removes nothing: the confirmation names what moves
      // instead of saying "restores 0 documents", which reads like a failure.
      mockVersions.current = [
        {
          version: 2,
          kind: 'dataset_config',
          created_by: 'user-1',
          create_time: 1762764000000,
        },
      ];
      mockCheckRestore.mockResolvedValue({ code: 0, data: plan });

      render(<DatasetVersionHistory datasetId="ds-1" />);

      fireEvent.click(
        screen.getByRole('button', { name: 'datasetVersion.restore' }),
      );

      await waitFor(() => expect(screen.getByText(key)).toBeInTheDocument());
    },
  );

  it('leaves the confirmation to its title when no document moves', async () => {
    // A version whose documents are all still present has nothing to say about
    // the document set: the title names it, and the dialog adds no sentence -
    // not one saying nothing changes, and not one about the chunk replay.
    mockVersions.current = [
      {
        version: 2,
        kind: 'upload',
        created_by: 'user-1',
        create_time: 1762764000000,
      },
    ];
    mockCheckRestore.mockResolvedValue({
      code: 0,
      data: { restored_documents: [], deleted_documents: [] },
    });

    render(<DatasetVersionHistory datasetId="ds-1" />);

    fireEvent.click(
      screen.getByRole('button', { name: 'datasetVersion.restore' }),
    );

    await waitFor(() =>
      expect(
        screen.getByText('datasetVersion.restoreConfirmTitle'),
      ).toBeInTheDocument(),
    );
    expect(
      screen.queryByText('datasetVersion.restoreConfirmNone'),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByText('datasetVersion.restoreConfirmChunkReplay'),
    ).not.toBeInTheDocument();
  });

  it('refreshes the list instead of failing loudly when the write is refused', async () => {
    mockListHead.current = 3;
    mockFreshHead.current = 3;
    mockVersions.current = [
      {
        version: 2,
        kind: 'dataset_config',
        created_by: 'user-1',
        create_time: 1762764000000,
      },
    ];
    mockRestore.mockRejectedValue(new Error('409'));

    render(<DatasetVersionHistory datasetId="ds-1" />);

    fireEvent.click(
      screen.getByRole('button', { name: 'datasetVersion.restore' }),
    );
    await confirmRestore();

    // The refusal is reported by the request layer; the panel just has to stop
    // showing a head it can no longer stand behind.
    await waitFor(() => expect(mockRestore).toHaveBeenCalled());
    expect(
      screen.getByRole('button', { name: 'datasetVersion.restore' }),
    ).toBeInTheDocument();
  });

  it('reads the next page when the list is scrolled to its end', async () => {
    // A full page is what says there is probably more; jsdom reports no layout,
    // so every scroll event reads as "at the end".
    mockVersions.current = Array.from({ length: 50 }, (_, index) => ({
      version: 50 - index,
      kind: 'dataset_config',
      created_by: 'user-1',
      create_time: 1762764000000,
    }));

    render(<DatasetVersionHistory datasetId="ds-1" />);

    const container = screen.getByRole('table').parentElement as HTMLElement;
    fireEvent.scroll(container);

    await waitFor(() =>
      expect(useFetchDatasetVersions).toHaveBeenCalledWith(
        'ds-1',
        expect.objectContaining({ limit: 100 }),
      ),
    );
  });

  it('reads nothing more when the page it has is not full', async () => {
    mockVersions.current = [
      {
        version: 2,
        kind: 'dataset_config',
        created_by: 'user-1',
        create_time: 1762764000000,
      },
    ];

    render(<DatasetVersionHistory datasetId="ds-1" />);

    const container = screen.getByRole('table').parentElement as HTMLElement;
    fireEvent.scroll(container);

    // The whole history is already in the list: scrolling to the end must not
    // ask the server for a page that cannot hold anything it does not have. The
    // hook runs on every render, so the assertion is on the limits it asked for
    // rather than on a call count.
    const limits = (
      useFetchDatasetVersions as unknown as jest.Mock
    ).mock.calls.map(([, options]) => options.limit);
    expect(limits.every((value) => value === 50)).toBe(true);
  });

  it('folds a long file list behind a toggle', () => {
    mockVersions.current = [];
    mockDetail.current = {
      version: 6,
      kind: 'upload',
      documents: Array.from({ length: 7 }, (_, index) => ({
        id: `doc-${index + 1}`,
        name: `file-${index + 1}.pdf`,
      })),
      changes: [],
      config: {},
    };

    render(<DatasetVersionHistory datasetId="ds-1" />);

    // The head of the set is listed, and the rest is behind the toggle rather
    // than cut off without saying so.
    expect(screen.getByText('file-1.pdf')).toBeInTheDocument();
    expect(screen.getByText('file-5.pdf')).toBeInTheDocument();
    expect(screen.queryByText('file-6.pdf')).not.toBeInTheDocument();
    expect(
      screen.getByText('datasetVersion.documentsShowAll'),
    ).toBeInTheDocument();

    fireEvent.click(screen.getByText('datasetVersion.documentsShowAll'));

    expect(screen.getByText('file-6.pdf')).toBeInTheDocument();
    expect(screen.getByText('file-7.pdf')).toBeInTheDocument();
    expect(
      screen.getByText('datasetVersion.documentsShowLess'),
    ).toBeInTheDocument();

    fireEvent.click(screen.getByText('datasetVersion.documentsShowLess'));

    expect(screen.queryByText('file-6.pdf')).not.toBeInTheDocument();
    expect(screen.getByText('file-1.pdf')).toBeInTheDocument();
  });
});
