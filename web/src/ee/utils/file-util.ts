/**
 * Enterprise extension of `@/utils/file-util`.
 *
 * The shared module stays byte-identical to upstream; enterprise callers import
 * this module when they need the helpers below.
 */
export * from '@/utils/file-util';

// Pure base64 conversion without compression or format change.
// Use this for general file attachments; for image resizing use transformFile2Base64.
export function fileToBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.readAsDataURL(file);
    reader.onload = () => {
      const result = reader.result as string;
      const base64 = result.split(',')[1];
      resolve(base64);
    };
    reader.onerror = reject;
  });
}
