import { v4 as uuidV4 } from 'uuid';

export * from '@/pages/admin/utils';

/** LDAP is an enterprise-only deployment option; OSS never enables it. */
export const USE_LDAP =
  import.meta.env.VITE_RAGFLOW_USE_LDAP === 'RAGFLOW_USE_LDAP';

type UUID = `${string}-${string}-${string}-${string}-${string}`;

let uuid = (): UUID => {
  /**
   * Crypto.prototype.randomUUID()
   * Secure contexts (HTTPS)
   * Baseline 2022
   */
  if (window.crypto && typeof window.crypto.randomUUID === 'function') {
    uuid = () => window.crypto.randomUUID();
  }
  // Insecure contexts, bring our own implementation
  else if (
    window.crypto &&
    typeof window.crypto.getRandomValues === 'function'
  ) {
    /**
     * Based on the section of `Crypto.prototype.randomUUID()`
     * @see https://w3c.github.io/webcrypto/#Crypto-method-randomUUID
     */
    const getData = () => {
      const data = [
        window.crypto.getRandomValues(new Uint8Array(4)),
        window.crypto.getRandomValues(new Uint8Array(2)),
        window.crypto.getRandomValues(new Uint8Array(2)),
        window.crypto.getRandomValues(new Uint8Array(2)),
        window.crypto.getRandomValues(new Uint8Array(6)),
      ];

      // Set the version bits to 0b0100xxxx (RFC 4122)
      data[2][0] = (data[2][0] & 0xf) | 0x40;
      // Set the variant bits to 0b10xxxxxx (RFC 4122)
      data[3][0] = (data[3][0] & 0x3f) | 0x80;

      return data;
    };

    /**
     * Unit8Array.prototype.toHex()
     * Baseline 2025
     * @see https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Uint8Array/toHex
     */
    // @ts-ignore
    if (typeof Uint8Array.prototype.toHex === 'function') {
      uuid = () =>
        getData()
          // @ts-ignore
          .map((d) => d.toHex())
          .join('-') as UUID;
    } else {
      uuid = () =>
        getData()
          .map((d) =>
            [...d].map((n) => n.toString(16).padStart(2, '0')).join(''),
          )
          .join('-') as UUID;
    }
  }
  // Fallback to uuid library
  else {
    uuid = () => uuidV4() as UUID;
  }

  return uuid();
};

export function uuidParts() {
  return uuid().split('-') as [string, string, string, string, string];
}

export const RESOURCE_PERMISSIONS = {
  dataset: ['enable', 'read', 'write', 'share'],
  chat: ['enable', 'read', 'write', 'share'],
  agent: ['enable', 'read', 'write', 'share'],
  search: ['enable', 'read', 'write', 'share'],
  file: ['enable', 'read', 'write', 'share'],
  team: ['enable', 'read', 'write', 'share'],
  memory: ['enable', 'read', 'write', 'share'],
  model_provider: ['enable'],
} satisfies Record<
  AdminServiceEe.RoleResourceName,
  AdminServiceEe.PermissionType[]
>;
