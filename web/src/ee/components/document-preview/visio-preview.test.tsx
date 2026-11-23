import { render, screen } from '@testing-library/react';
import { VisioPreview } from './visio-preview';

jest.mock('react', () => {
  const actual = jest.requireActual('react');
  return {
    ...actual,
    lazy: () => {
      const MockLazy = (props: Record<string, unknown>) => {
        const Comp = require('./visio-previewer').default;
        return <Comp {...props} />;
      };
      return MockLazy;
    },
  };
});

jest.mock('./visio-previewer', () => ({
  __esModule: true,
  default: (props: { url?: string }) => (
    <div data-testid="mock-visio-previewer" data-url={props.url} />
  ),
}));

describe('VisioPreview EE wrapper', () => {
  it('renders nothing for non-Visio extensions', () => {
    const { container } = render(
      <VisioPreview fileType="pdf" url="/api/v1/documents/doc1/preview" />,
    );
    expect(container).toBeEmptyDOMElement();
  });

  it('renders VisioPreviewer lazily for .vsdx files', async () => {
    render(
      <VisioPreview
        fileType="VSDX"
        url="/api/v1/documents/doc1/preview"
        positions={[[2, 4, 0, 0, 0]]}
      />,
    );
    expect(await screen.findByTestId('mock-visio-previewer')).toHaveAttribute(
      'data-url',
      '/api/v1/documents/doc1/preview',
    );
  });
});
