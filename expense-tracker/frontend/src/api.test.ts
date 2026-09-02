import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createUploadUrl, putFile } from './api';
import type { ReceiptMeta, UploadTicket } from './types';

// Same-origin default: nginx proxies /api/* to the CreateUploadURL service.
const DEFAULT_ENDPOINT = '/api/upload-url';

beforeEach(() => {
  // No VITE_API_BASE stubbed -> api.ts must fall back to the relative "/api".
});

afterEach(() => {
  vi.unstubAllEnvs();
  vi.restoreAllMocks();
});

const meta: ReceiptMeta = {
  vendor: 'Starbucks',
  amount: 4.5,
  currency: 'USD',
  date: '2026-09-01',
  ext: 'jpg',
  contentType: 'image/jpeg',
};

const ticket: UploadTicket = {
  uploadUrl: 'https://storage.googleapis.com/signed-put-url',
  objectName: 'receipts/2026-09-01_starbucks_4.50_a1b2c3.jpg',
  method: 'PUT',
  headers: { 'Content-Type': 'image/jpeg' },
};

describe('createUploadUrl', () => {
  it('POSTs metadata as JSON to the same-origin /api/upload-url and returns the ticket', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ticket,
    });
    vi.stubGlobal('fetch', fetchMock);

    const result = await createUploadUrl(meta);

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe(DEFAULT_ENDPOINT);
    expect(init.method).toBe('POST');
    expect(init.headers['Content-Type']).toBe('application/json');
    expect(JSON.parse(init.body)).toEqual(meta);
    expect(result).toEqual(ticket);
  });

  it('honours VITE_API_BASE as the proxy base for the upload-url request', async () => {
    vi.stubEnv('VITE_API_BASE', 'http://localhost:8080');
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ticket,
    });
    vi.stubGlobal('fetch', fetchMock);

    await createUploadUrl(meta);

    const [url] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost:8080/upload-url');
  });

  it('throws with the server error message on a non-2xx response', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: false,
      status: 400,
      json: async () => ({ error: 'invalid amount' }),
    });
    vi.stubGlobal('fetch', fetchMock);

    await expect(createUploadUrl(meta)).rejects.toThrow('invalid amount');
  });
});

describe('putFile', () => {
  it('PUTs the raw file with the content-type header', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 200 });
    vi.stubGlobal('fetch', fetchMock);
    const file = new File(['bytes'], 'receipt.jpg', { type: 'image/jpeg' });

    await putFile(ticket.uploadUrl, file, 'image/jpeg');

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe(ticket.uploadUrl);
    expect(init.method).toBe('PUT');
    expect(init.body).toBe(file);
    expect(init.headers['Content-Type']).toBe('image/jpeg');
  });

  it('throws on a non-2xx response', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: false, status: 403 });
    vi.stubGlobal('fetch', fetchMock);
    const file = new File(['bytes'], 'receipt.jpg', { type: 'image/jpeg' });

    await expect(putFile(ticket.uploadUrl, file, 'image/jpeg')).rejects.toThrow();
  });
});
