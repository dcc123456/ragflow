import { z } from 'zod';

export const additionalParserGlobalOptions = z.object({
  remove_low_contrast_text: z.boolean().optional(),
});
