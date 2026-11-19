import PdfPreviewer, {
  type IProps,
} from '@/components/document-preview/pdf-preview';
import { isCadDocumentExtension } from '@/ee/utils/cad-document';

type CadPreviewProps = IProps & {
  fileType: string;
};

export function CadPreview({ fileType, ...pdfProps }: CadPreviewProps) {
  if (!isCadDocumentExtension(fileType)) {
    return null;
  }

  return (
    <section className="h-full">
      <PdfPreviewer {...pdfProps} />
    </section>
  );
}
