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

import { Button } from '@/components/ui/button';
import { useTranslate } from '@/hooks/common-hooks';
import { ArrowLeft } from 'lucide-react';
import { useEffect, useId, useState } from 'react';

// Official Feishu QR login SDK, loaded from the Feishu static CDN. Keep the
// URL in sync with the QR SDK documentation on open.feishu.cn. The script
// exposes window.QRLogin and renders the QR code for the goto authorization
// URL inside the container element; nothing about it is bundled locally.
const QR_SDK_SRC =
  'https://sf3-cn.feishucdn.com/obj/feishu-static/lark/passport/qrcode/LarkSSOSDKWebQRCode-1.0.2.js';

interface QRLoginOptions {
  id: string;
  goto: string;
  width?: string;
  height?: string;
  style?: string;
}

interface QRLoginInstance {
  matchOrigin(origin: string): boolean;
  matchData(data: unknown): data is { tmp_code: string };
}

declare global {
  interface Window {
    QRLogin?: (options: QRLoginOptions) => QRLoginInstance;
  }
}

let sdkPromise: Promise<NonNullable<Window['QRLogin']>> | null = null;

// Load the SDK script once per page; concurrent mounts share the promise.
const loadQrSdk = () => {
  if (sdkPromise) {
    return sdkPromise;
  }

  sdkPromise = new Promise((resolve, reject) => {
    if (window.QRLogin) {
      resolve(window.QRLogin);
      return;
    }

    const script = document.createElement('script');
    script.src = QR_SDK_SRC;
    script.async = true;
    script.onload = () => {
      if (window.QRLogin) {
        resolve(window.QRLogin);
      } else {
        sdkPromise = null;
        reject(new Error('Feishu QR SDK loaded but window.QRLogin is missing'));
      }
    };
    script.onerror = () => {
      sdkPromise = null;
      reject(new Error('Failed to load the Feishu QR SDK'));
    };
    document.head.appendChild(script);
  });
  return sdkPromise;
};

type FeishuQrLoginProps = {
  // Authorization URL from the initiate endpoint, with the state baked in.
  // The SDK renders the QR code for it and the confirmation jump re-opens it
  // with tmp_code appended; the server then redirects to the callback.
  authUrl: string;
  onBack: () => void;
};

export default function FeishuQrLogin({ authUrl, onBack }: FeishuQrLoginProps) {
  const { t } = useTranslate('login');
  const [failed, setFailed] = useState(false);
  const containerId = `feishu-qr-${useId().replace(/:/g, '')}`;

  useEffect(() => {
    let cancelled = false;
    let removeListener = () => {};

    loadQrSdk()
      .then((QRLogin) => {
        if (cancelled) {
          return;
        }

        const qr = QRLogin({
          id: containerId,
          goto: authUrl,
          width: '300',
          height: '300',
        });

        const onMessage = (event: MessageEvent) => {
          if (!qr.matchOrigin(event.origin)) {
            return;
          }
          // The SDK versions differ on the payload: older ones post the
          // tmp_code as a bare string, newer ones wrap it in { tmp_code }.
          const tmpCode =
            typeof event.data === 'string'
              ? event.data
              : (event.data as { tmp_code?: string })?.tmp_code;
          if (!tmpCode) {
            return;
          }
          window.removeEventListener('message', onMessage);
          removeListener = () => {};
          window.location.href = `${authUrl}&tmp_code=${tmpCode}`;
        };
        window.addEventListener('message', onMessage);
        removeListener = () => window.removeEventListener('message', onMessage);
      })
      .catch(() => {
        if (!cancelled) {
          setFailed(true);
        }
      });

    return () => {
      cancelled = true;
      removeListener();
    };
  }, [authUrl, containerId]);

  return (
    <div className="flex flex-col items-center gap-4 py-4">
      <p className="text-sm text-text-secondary">{t('feishuQrTitle')}</p>
      {failed ? (
        <p className="max-w-72 text-center text-sm text-state-error">
          {t('feishuQrFailed')}
        </p>
      ) : (
        <div id={containerId} />
      )}
      <Button variant="ghost" size="sm" onClick={onBack}>
        <ArrowLeft className="size-4" />
        {t('back')}
      </Button>
    </div>
  );
}
