import type { ReceiptMeta, UploadTicket } from './types';

/**
 * Base path for the upload-URL request. Defaults to the same-origin relative
 * "/api", which nginx reverse-proxies to the CreateUploadURL Cloud Run service
 * (option B: same-origin, no CORS). Override with VITE_API_BASE for local dev.
 */
function apiBase(): string {
  const base = import.meta.env.VITE_API_BASE as string | undefined;
  return (base ?? '/api').replace(/\/$/, '');
}

function endpoint(): string {
  return `${apiBase()}/upload-url`;
}

/**
 * Request a V4 signed PUT URL from the CreateUploadURL Cloud Function.
 * Throws Error(json.error) on a non-2xx response.
 */
export async function createUploadUrl(meta: ReceiptMeta): Promise<UploadTicket> {
  const res = await fetch(endpoint(), {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(meta),
  });

  if (!res.ok) {
    let message = `Upload URL request failed (HTTP ${res.status})`;
    try {
      const body = await res.json();
      if (body && typeof body.error === 'string') {
        message = body.error;
      }
    } catch {
      // non-JSON error body; keep the generic message
    }
    throw new Error(message);
  }

  return (await res.json()) as UploadTicket;
}

/**
 * Upload the raw file bytes directly to GCS via the signed PUT URL.
 * Throws on a non-2xx response.
 */
export async function putFile(
  uploadUrl: string,
  file: File,
  contentType: string
): Promise<void> {
  const res = await fetch(uploadUrl, {
    method: 'PUT',
    headers: { 'Content-Type': contentType },
    body: file,
  });

  if (!res.ok) {
    throw new Error(`File upload failed (HTTP ${res.status})`);
  }
}
