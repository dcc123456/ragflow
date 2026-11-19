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

import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import ChunkVersionHistory from '.';

// Only the interpolating keys are exercised here, so the stub resolves
// `{{user}}` and leaves every other key as its name.
jest.mock('react-i18next', () => {
  // The real module is kept so the app wide i18n instance still initialises
  // when the component's import chain pulls in `@/locales/config`.
  const actual = jest.requireActual('react-i18next');
  return {
    ...actual,
    useTranslation: () => ({
      t: (key: string, options?: { user?: string; version?: number }) => {
        if (options?.user !== undefined) {
          return `${key}: ${options.user}`;
        }
        if (options?.version !== undefined) {
          return `${key}: v${options.version}`;
        }
        return key;
      },
    }),
  };
});

// `any` on purpose: this file goes through esbuild-jest's babel pass, which
// refuses to strip an imported binding that is also used in a type annotation.
const versionsState: { current: any[] } = { current: [] };
// What the panel asked to compare, so a test can see which pair it opened on.
const diffArgs: { current: any[] } = { current: [] };
// The comparison the server would answer with.
const mockDiffState: { current: any } = { current: undefined };
// The version detail the panel previews.
const mockDetailState: { current: any } = { current: undefined };
// What the restore action was asked to write back.
const mockRestoreChunkVersion = jest.fn();
// What the delete action answered with: { code, message }, where a non-zero
// code is the server's refusal.
const mockDeleteChunkVersion = jest.fn();

const versionRow = (version: number, latest = false) => ({
  version,
  created_by: '',
  restored_from: 0,
  create_date: `2026-11-1${version}T10:00:00`,
  latest,
  kind: 'edit',
});

// Most tests only care about the version list; a comparison is opt-in.
beforeEach(() => {
  mockDiffState.current = undefined;
  mockDetailState.current = undefined;
  mockRestoreChunkVersion.mockReset();
  mockDeleteChunkVersion.mockReset();
});

jest.mock('@/ee/hooks/use-chunk-version', () => ({
  __esModule: true,
  useFetchChunkVersions: () => ({
    versions: versionsState.current,
    loading: false,
  }),
  useFetchChunkVersion: () => ({
    detail: mockDetailState.current,
    loading: false,
  }),
  useFetchChunkVersionDiff: (...args: any[]) => {
    diffArgs.current = args;
    return {
      diff: mockDiffState.current,
      loading: false,
      missingVersion: false,
    };
  },
  useRestoreChunkVersion: () => ({
    restoreChunkVersion: mockRestoreChunkVersion,
    loading: false,
  }),
  useDeleteChunkVersion: () => ({
    deleteChunkVersion: mockDeleteChunkVersion,
    loading: false,
  }),
}));

// A version's picture is read through its own endpoint, which needs the session
// header; the test only checks which versions the comparison asked for.
jest.mock('@/components/image', () => {
  const React = require('react');
  return {
    AuthenticatedImg: ({ src }: { src?: string }) =>
      React.createElement('img', { 'data-testid': 'version-image', src }),
  };
});

jest.mock('@/hooks/route-hook', () => ({
  useGetKnowledgeSearchParams: () => ({
    type: '',
    documentId: 'doc-1',
    knowledgeId: 'kb-1',
  }),
}));

describe('chunk version history actions', () => {
  beforeEach(() => {
    versionsState.current = [versionRow(3, true), versionRow(2), versionRow(1)];
  });

  it('shows both actions without switching the panel to preview', () => {
    // The panel opens on the comparison view. The actions act on the row the list
    // has selected rather than on the view, so both are visible either way
    // instead of appearing only once a single version is being read.
    render(<ChunkVersionHistory chunkId="c-1" docId="doc-1" />);

    expect(
      screen.getByRole('button', { name: 'chunk.deleteVersion' }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole('button', { name: 'chunk.restoreVersion' }),
    ).toBeInTheDocument();
  });

  it('asks before restoring a version', async () => {
    mockRestoreChunkVersion.mockResolvedValue(0);
    render(<ChunkVersionHistory chunkId="c-1" docId="doc-1" />);

    fireEvent.click(screen.getByRole('button', { name: /^v2/ }));
    fireEvent.click(
      screen.getByRole('button', { name: 'chunk.restoreVersion' }),
    );

    // The confirmation names the version, and nothing has been written yet.
    expect(
      await screen.findByText('chunk.restoreVersionTitle: v2'),
    ).toBeInTheDocument();
    expect(
      screen.getByText('chunk.restoreVersionDescription: v2'),
    ).toBeInTheDocument();
    expect(mockRestoreChunkVersion).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole('button', { name: 'common.confirm' }));

    await waitFor(() =>
      expect(mockRestoreChunkVersion).toHaveBeenCalledWith({
        kb_id: 'kb-1',
        doc_id: 'doc-1',
        chunk_id: 'c-1',
        version: 2,
      }),
    );
  });

  it('keeps the confirmation open when the restore is refused', async () => {
    mockRestoreChunkVersion.mockResolvedValue(102);
    render(<ChunkVersionHistory chunkId="c-1" docId="doc-1" />);

    fireEvent.click(screen.getByRole('button', { name: /^v2/ }));
    fireEvent.click(
      screen.getByRole('button', { name: 'chunk.restoreVersion' }),
    );
    await screen.findByText('chunk.restoreVersionTitle: v2');

    fireEvent.click(screen.getByRole('button', { name: 'common.confirm' }));

    await waitFor(() =>
      expect(mockRestoreChunkVersion).toHaveBeenCalledTimes(1),
    );
    // A refusal leaves the dialog up, so the reader can pick another version
    // behind it instead of having to reopen the action.
    expect(
      screen.getByText('chunk.restoreVersionTitle: v2'),
    ).toBeInTheDocument();
  });

  it('explains a refused delete in its own dialog', async () => {
    mockDeleteChunkVersion.mockResolvedValue({
      code: 409,
      message:
        'version 2 is the state dataset version 3 restores from and cannot be deleted',
    });
    render(<ChunkVersionHistory chunkId="c-1" docId="doc-1" />);

    fireEvent.click(screen.getByRole('button', { name: /^v2/ }));
    fireEvent.click(
      screen.getByRole('button', { name: 'chunk.deleteVersion' }),
    );
    await screen.findByText('chunk.deleteVersionTitle: v2');

    fireEvent.click(screen.getByRole('button', { name: 'common.confirm' }));

    // The confirmation gives way to a dialog carrying the server's sentence: it
    // names the state that still needs this row, which is the whole point of the
    // refusal and more than a toast could say (the request layer only saw 409).
    expect(
      await screen.findByText(
        'version 2 is the state dataset version 3 restores from and cannot be deleted',
      ),
    ).toBeInTheDocument();
    expect(screen.getByText('chunk.versionNotDeletable')).toBeInTheDocument();
    expect(
      screen.queryByText('chunk.deleteVersionTitle: v2'),
    ).not.toBeInTheDocument();
  });

  it('enables both actions when an editable older version is selected', () => {
    render(<ChunkVersionHistory chunkId="c-1" docId="doc-1" />);

    fireEvent.click(screen.getByRole('button', { name: /^v2/ }));

    expect(
      screen.getByRole('button', { name: 'chunk.restoreVersion' }),
    ).toBeEnabled();
    expect(
      screen.getByRole('button', { name: 'chunk.deleteVersion' }),
    ).toBeEnabled();
  });
});

describe('chunk version history picture comparison', () => {
  beforeEach(() => {
    versionsState.current = [versionRow(3, true), versionRow(2), versionRow(1)];
  });

  it('compares the two pictures instead of the image ids', () => {
    mockDiffState.current = {
      chunk_id: 'c-1',
      from: 1,
      to: 2,
      identical: false,
      fields: [
        { field: 'img_id', kind: 'scalar', before: 'img-a', after: 'img-b' },
      ],
    };

    render(<ChunkVersionHistory chunkId="c-1" docId="doc-1" />);

    const pictures = screen.getAllByTestId('version-image');
    expect(pictures).toHaveLength(2);
    expect(pictures[0]).toHaveAttribute(
      'src',
      expect.stringContaining('/versions/1/image'),
    );
    expect(pictures[1]).toHaveAttribute(
      'src',
      expect.stringContaining('/versions/2/image'),
    );
    // The ids are storage keys, not something a reader compares.
    expect(screen.queryByText('img-a')).not.toBeInTheDocument();
  });

  it('says no image where a version recorded none', () => {
    mockDiffState.current = {
      chunk_id: 'c-1',
      from: 1,
      to: 2,
      identical: false,
      fields: [{ field: 'img_id', kind: 'scalar', before: '', after: 'img-b' }],
    };

    render(<ChunkVersionHistory chunkId="c-1" docId="doc-1" />);

    expect(screen.getAllByTestId('version-image')).toHaveLength(1);
    expect(screen.getByText('chunk.noImage')).toBeInTheDocument();
  });
});

describe('chunk version history visibility comparison', () => {
  beforeEach(() => {
    versionsState.current = [versionRow(3, true), versionRow(2), versionRow(1)];
  });

  it('reads the switch as a state instead of the stored number', () => {
    mockDiffState.current = {
      chunk_id: 'c-1',
      from: 1,
      to: 2,
      identical: false,
      fields: [{ field: 'available_int', kind: 'scalar', before: 1, after: 0 }],
    };

    render(<ChunkVersionHistory chunkId="c-1" docId="doc-1" />);

    expect(screen.getByText('chunk.diffFieldVisibility')).toBeInTheDocument();
    expect(screen.getByText('chunk.enabled')).toBeInTheDocument();
    expect(screen.getByText('chunk.disabled')).toBeInTheDocument();
    // The raw values are what the server sends, not what a reader compares.
    expect(screen.queryByText('1')).not.toBeInTheDocument();
    expect(screen.queryByText('0')).not.toBeInTheDocument();
  });
});

describe('chunk version history content preview', () => {
  beforeEach(() => {
    versionsState.current = [versionRow(3, true), versionRow(2), versionRow(1)];
  });

  it('shows the text of a version without the picture it was cut from', () => {
    // A PDF chunk carries the picture of the region it came from as well as the
    // text; the history reads as text, so the picture is not rendered.
    mockDetailState.current = {
      kind: 'edit',
      latest: false,
      chunk: {
        // A parsers embeds the picture of the page region in the chunk body, and
        // the version keeps it beside the chunk's own img_id.
        content_with_weight:
          '<p>text kept from the PDF</p><img src="data:image/png;base64,AAAA" />',
        img_id: 'img-1',
      },
    };

    const { container } = render(
      <ChunkVersionHistory chunkId="c-1" docId="doc-1" />,
    );
    fireEvent.click(
      screen.getByRole('button', { name: 'chunk.versionContent' }),
    );

    expect(screen.getByText('text kept from the PDF')).toBeInTheDocument();
    expect(container.querySelector('img')).toBeNull();
    expect(container.textContent).not.toContain('base64');
  });

  it('shows the picture an image chunk holds beside its text', () => {
    // An image chunk's text is what a match reads; the picture is what the
    // chunk is, so the preview shows both.
    mockDetailState.current = {
      kind: 'edit',
      latest: false,
      chunk: {
        content_with_weight: 'a caption',
        doc_type_kwd: 'image',
        img_id: 'img-1',
      },
    };

    render(<ChunkVersionHistory chunkId="c-1" docId="doc-1" />);
    fireEvent.click(
      screen.getByRole('button', { name: 'chunk.versionContent' }),
    );

    expect(screen.getByText('a caption')).toBeInTheDocument();
    const pictures = screen.getAllByTestId('version-image');
    expect(pictures).toHaveLength(1);
    expect(pictures[0]).toHaveAttribute(
      'src',
      expect.stringContaining('/versions/3/image'),
    );
  });

  it('names the switch a version recorded', () => {
    // Only the hiding value is stored, so a version without the key was visible.
    mockDetailState.current = {
      kind: 'edit',
      latest: false,
      chunk: { content_with_weight: 'text', available_int: 0 },
    };

    const hidden = render(<ChunkVersionHistory chunkId="c-1" docId="doc-1" />);
    fireEvent.click(
      screen.getByRole('button', { name: 'chunk.versionContent' }),
    );
    expect(screen.getByText('chunk.diffFieldVisibility')).toBeInTheDocument();
    expect(screen.getByText('chunk.disabled')).toBeInTheDocument();

    hidden.unmount();
    mockDetailState.current = {
      kind: 'edit',
      latest: false,
      chunk: { content_with_weight: 'text' },
    };
    render(<ChunkVersionHistory chunkId="c-1" docId="doc-1" />);
    fireEvent.click(
      screen.getByRole('button', { name: 'chunk.versionContent' }),
    );
    expect(screen.getByText('chunk.enabled')).toBeInTheDocument();
  });
});

describe('chunk version history list', () => {
  beforeEach(() => {
    versionsState.current = [];
  });

  it('names the actor of every version, falling back to the id', () => {
    versionsState.current = [
      {
        version: 3,
        created_by: 'user-3',
        created_by_name: 'Alice',
        restored_from: 0,
        create_date: '2026-11-10T10:00:00',
        latest: true,
        kind: 'edit',
      },
      {
        // An edit whose user row no longer exists: the server leaves the name
        // empty, so the raw id is the only thing left to show.
        version: 2,
        created_by: 'deleted-user-id',
        restored_from: 0,
        create_date: '2026-11-09T10:00:00',
        latest: false,
        kind: 'restore',
      },
      {
        // The parser's baseline carries no user, only the "system" marker.
        version: 0,
        created_by: 'system',
        restored_from: 0,
        create_date: '2026-11-08T10:00:00',
        latest: false,
        kind: 'parse',
      },
    ];

    render(<ChunkVersionHistory chunkId="chunk-1" docId="doc-1" />);

    // Every row answers "who": the nickname the server resolved, the id when it
    // could not be resolved, and the localised label for a system written row.
    expect(screen.getByText('chunk.changedBy: Alice')).toBeInTheDocument();
    expect(
      screen.getByText('chunk.changedBy: deleted-user-id'),
    ).toBeInTheDocument();
    expect(
      screen.getByText('chunk.changedBy: chunk.systemActor'),
    ).toBeInTheDocument();
  });

  it('leaves the actor line out when a version names nobody', () => {
    versionsState.current = [
      {
        version: 0,
        created_by: '',
        restored_from: 0,
        create_date: '2026-11-08T10:00:00',
        latest: true,
        kind: 'edit',
      },
    ];

    render(<ChunkVersionHistory chunkId="chunk-1" docId="doc-1" />);

    expect(screen.queryByText(/chunk.changedBy/)).not.toBeInTheDocument();
  });
});

describe('chunk version history comparison', () => {
  beforeEach(() => {
    diffArgs.current = [];
    versionsState.current = [
      {
        version: 3,
        created_by: '',
        restored_from: 0,
        create_date: '2026-11-10T10:00:00',
        latest: true,
        kind: 'edit',
      },
      {
        version: 2,
        created_by: '',
        restored_from: 0,
        create_date: '2026-11-09T10:00:00',
        latest: false,
        kind: 'edit',
      },
      {
        version: 0,
        created_by: '',
        restored_from: 0,
        create_date: '2026-11-08T10:00:00',
        latest: false,
        kind: 'parse',
      },
    ];
  });

  it('opens on the changes of the selected version', () => {
    render(<ChunkVersionHistory chunkId="chunk-1" docId="doc-1" />);

    // The panel lands on Changes, with the newest version on the right and the
    // state it replaced as the base.
    expect(diffArgs.current).toEqual(['chunk-1', 'doc-1', 2, 3]);
    // Only the base is a dropdown: the version being explained is whichever row
    // the list has selected, so it is shown rather than offered.
    expect(screen.getAllByRole('combobox')).toHaveLength(1);
  });

  it('compares a selected version against the one before it', () => {
    render(<ChunkVersionHistory chunkId="chunk-1" docId="doc-1" />);

    // v2 replaced v0, so "the changes of v2" is that pair, not v2 against
    // whatever the previous view happened to be comparing. The row is picked by
    // role: "v2" is also the label the base dropdown shows once v3 is selected.
    fireEvent.click(screen.getByRole('button', { name: /^v2\b/ }));

    expect(diffArgs.current).toEqual(['chunk-1', 'doc-1', 0, 2]);
  });
});

describe('chunk version history default selection', () => {
  beforeEach(() => {
    diffArgs.current = [];
  });

  it('lands on the version the last save wrote, not the cached newest', () => {
    versionsState.current = [versionRow(3, true), versionRow(2), versionRow(0)];
    const { rerender } = render(
      <ChunkVersionHistory chunkId="chunk-1" docId="doc-1" />,
    );

    // The panel opened on the list cached before the edit.
    expect(diffArgs.current).toEqual(['chunk-1', 'doc-1', 2, 3]);

    // The refetch brings the version the save just wrote. Nothing has been
    // picked, so the selection follows the newest row.
    versionsState.current = [
      versionRow(4, true),
      versionRow(3),
      versionRow(2),
      versionRow(0),
    ];
    rerender(<ChunkVersionHistory chunkId="chunk-1" docId="doc-1" />);

    expect(diffArgs.current).toEqual(['chunk-1', 'doc-1', 3, 4]);
  });

  it('keeps the version the user picked across a refresh', () => {
    versionsState.current = [versionRow(3, true), versionRow(2), versionRow(0)];
    const { rerender } = render(
      <ChunkVersionHistory chunkId="chunk-1" docId="doc-1" />,
    );
    fireEvent.click(screen.getByRole('button', { name: /^v2\b/ }));
    expect(diffArgs.current).toEqual(['chunk-1', 'doc-1', 0, 2]);

    versionsState.current = [
      versionRow(4, true),
      versionRow(3),
      versionRow(2),
      versionRow(0),
    ];
    rerender(<ChunkVersionHistory chunkId="chunk-1" docId="doc-1" />);

    // A new version existing is not a reason to move a reader off the one they
    // are looking at.
    expect(diffArgs.current).toEqual(['chunk-1', 'doc-1', 0, 2]);
  });
});

describe('chunk version changes panel', () => {
  it('says there is nothing to review instead of listing unchanged fields', () => {
    // A version compared with itself: the server reports every list field as
    // unchanged, which is context while something else changed and pure noise
    // when nothing did.
    mockDiffState.current = {
      identical: true,
      fields: [
        {
          field: 'important_kwd',
          kind: 'list',
          changed: false,
          added: [],
          removed: [],
          unchanged: ['keyword'],
        },
      ],
    };
    versionsState.current = [versionRow(1, true)];

    render(<ChunkVersionHistory chunkId="chunk-1" docId="doc-1" />);

    expect(screen.getByText('chunk.noDiff')).toBeInTheDocument();
    expect(screen.queryByText('chunk.keyword')).not.toBeInTheDocument();
  });

  it('drops a list field both versions kept instead of marking it unchanged', () => {
    // The server reports the whole keyword set either way; a list neither side
    // moved is not a change, so the panel only lists what moved.
    mockDiffState.current = {
      identical: false,
      fields: [
        { field: 'available_int', kind: 'scalar', before: 1, after: 0 },
        {
          field: 'important_kwd',
          kind: 'list',
          changed: false,
          added: [],
          removed: [],
          unchanged: ['keyword'],
        },
        {
          field: 'question_kwd',
          kind: 'list',
          changed: true,
          added: ['new question'],
          removed: [],
          unchanged: [],
        },
      ],
    };
    versionsState.current = [versionRow(1, true)];

    render(<ChunkVersionHistory chunkId="chunk-1" docId="doc-1" />);

    expect(screen.getByText('chunk.diffFieldVisibility')).toBeInTheDocument();
    // The untouched keyword list is gone, so the "Unchanged" badge is too.
    expect(screen.queryByText('chunk.keyword')).not.toBeInTheDocument();
    expect(screen.queryByText('chunk.diffUnchanged')).not.toBeInTheDocument();
    // A list that did move stays, next to what it gained.
    expect(screen.getByText('chunk.question')).toBeInTheDocument();
    expect(screen.getByText('+new question')).toBeInTheDocument();
  });

  it('drops the coordinates row a server may still report', () => {
    // The server no longer compares position_int, but a deployment running the
    // older build still sends it: the panel must not turn that into a row, and a
    // difference in nothing else has to read as "nothing to review".
    mockDiffState.current = {
      identical: false,
      fields: [{ field: 'position_int', kind: 'json', changed: true }],
    };
    versionsState.current = [versionRow(1, true)];

    render(<ChunkVersionHistory chunkId="chunk-1" docId="doc-1" />);

    expect(screen.getByText('chunk.noDiff')).toBeInTheDocument();
    expect(screen.queryByText('position_int')).not.toBeInTheDocument();
  });
});
describe('chunk version history delete marker', () => {
  // A removal marker holds no state: it records that the chunk was gone from
  // that version on, so neither pane should present an empty chunk.
  beforeEach(() => {
    versionsState.current = [
      { ...versionRow(3, true), kind: 'delete' },
      versionRow(2),
      versionRow(1),
    ];
    mockDetailState.current = {
      version: 3,
      kind: 'delete',
      latest: true,
      restored_from: 0,
      chunk: {},
    };
  });

  it('reports the removal in the changes pane instead of comparing an absent chunk', () => {
    mockDiffState.current = {
      identical: false,
      fields: [{ field: 'content_with_weight', kind: 'text', diff: '' }],
    };

    render(<ChunkVersionHistory chunkId="c-1" docId="doc-1" />);

    expect(screen.getByText('chunk.versionChunkDeleted')).toBeInTheDocument();
    expect(screen.queryByText('chunk.noDiff')).not.toBeInTheDocument();
    expect(
      screen.queryByText('chunk.diffFieldContent'),
    ).not.toBeInTheDocument();
  });

  it('reports the removal in the content pane instead of an empty body', () => {
    render(<ChunkVersionHistory chunkId="c-1" docId="doc-1" />);
    fireEvent.click(
      screen.getByRole('button', { name: 'chunk.versionContent' }),
    );

    expect(screen.getByText('chunk.versionChunkDeleted')).toBeInTheDocument();
    expect(screen.queryByText('chunk.versionEmpty')).not.toBeInTheDocument();
  });
});
