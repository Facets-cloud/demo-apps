import type { ReceiptMeta, UploadTicket, Summary, ExpenseItem } from './types';

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
 * Read the running summary aggregate (proves the Pub/Sub → SummaryConsumer path).
 * Same-origin GET, reverse-proxied by nginx to the backend.
 */
export async function getSummary(): Promise<Summary> {
  const res = await fetch(`${apiBase()}/summary`);
  if (!res.ok) {
    throw new Error(`Summary request failed (HTTP ${res.status})`);
  }
  return (await res.json()) as Summary;
}

/**
 * Read the most recent expenses (proves the GCS-finalize → ReceiptUploaded path).
 */
export async function getExpenses(): Promise<ExpenseItem[]> {
  const res = await fetch(`${apiBase()}/expenses`);
  if (!res.ok) {
    throw new Error(`Expenses request failed (HTTP ${res.status})`);
  }
  return (await res.json()) as ExpenseItem[];
}

/**
 * Get a short-lived signed GET URL to view a private receipt image (the mirror
 * of the upload signing). Returns the URL string for use as an <img src>.
 */
export async function getReceiptUrl(object: string): Promise<string> {
  const res = await fetch(`${apiBase()}/receipt-url?object=${encodeURIComponent(object)}`);
  if (!res.ok) {
    throw new Error(`Receipt URL request failed (HTTP ${res.status})`);
  }
  const body = (await res.json()) as { url: string };
  return body.url;
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
