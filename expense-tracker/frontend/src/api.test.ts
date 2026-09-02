import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createUploadUrl, putFile } from './api';
import type { ReceiptMeta, UploadTicket } from './types';

const ENDPOINT = 'http://localhost:8080';

beforeEach(() => {
  vi.stubEnv('VITE_UPLOAD_URL_ENDPOINT', ENDPOINT);
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
  it('POSTs metadata as JSON to the endpoint and returns the ticket', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ticket,
    });
    vi.stubGlobal('fetch', fetchMock);

    const result = await createUploadUrl(meta);

    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe(ENDPOINT);
    expect(init.method).toBe('POST');
    expect(init.headers['Content-Type']).toBe('application/json');
    expect(JSON.parse(init.body)).toEqual(meta);
    expect(result).toEqual(ticket);
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
