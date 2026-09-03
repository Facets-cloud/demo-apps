import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import App from './App';
import * as api from './api';
import type { UploadTicket, ExpenseItem } from './types';

vi.mock('./api');

const ticket: UploadTicket = {
  uploadUrl: 'https://storage.googleapis.com/signed-put-url',
  objectName: 'receipts/2026-09-01_starbucks_4.50_a1b2c3.jpg',
  method: 'PUT',
  headers: { 'Content-Type': 'image/jpeg' },
};

const expenseItem: ExpenseItem = {
  id: '1',
  vendor: 'starbucks',
  amount_cents: 450,
  currency: 'USD',
  spent_on: '2026-09-01',
  source_object: 'receipts/2026-09-01_starbucks_4.50_a1b2c3.jpg',
  created_at: '2026-09-01T10:00:00Z',
};

beforeEach(() => {
  vi.mocked(api.createUploadUrl).mockResolvedValue(ticket);
  vi.mocked(api.putFile).mockResolvedValue(undefined);
  vi.mocked(api.getSummary).mockResolvedValue({ count: 0, total_cents: 0 });
  vi.mocked(api.getExpenses).mockResolvedValue([]);
  vi.mocked(api.getReceiptUrl).mockResolvedValue('https://storage.googleapis.com/signed-get-url');
});

afterEach(() => {
  vi.clearAllMocks();
});

async function fillForm() {
  const user = userEvent.setup();
  const file = new File(['bytes'], 'receipt.jpg', { type: 'image/jpeg' });
  await user.upload(screen.getByLabelText(/receipt image/i), file);
  await user.clear(screen.getByLabelText(/vendor/i));
  await user.type(screen.getByLabelText(/vendor/i), 'Starbucks');
  await user.clear(screen.getByLabelText(/amount/i));
  await user.type(screen.getByLabelText(/amount/i), '4.50');
  await user.clear(screen.getByLabelText(/date/i));
  await user.type(screen.getByLabelText(/date/i), '2026-09-01');
  return { user, file };
}

describe('<App />', () => {
  it('disables submit until a file is selected', () => {
    render(<App />);
    expect(screen.getByRole('button', { name: /upload/i })).toBeDisabled();
  });

  it('creates an upload url, PUTs the file, and shows the object name', async () => {
    render(<App />);
    const { user, file } = await fillForm();

    await user.click(screen.getByRole('button', { name: /upload/i }));

    await waitFor(() => {
      expect(api.createUploadUrl).toHaveBeenCalledTimes(1);
    });
    expect(api.createUploadUrl).toHaveBeenCalledWith(
      expect.objectContaining({
        vendor: 'Starbucks',
        amount: 4.5,
        currency: 'USD',
        date: '2026-09-01',
        ext: 'jpg',
        contentType: 'image/jpeg',
      })
    );
    expect(api.putFile).toHaveBeenCalledWith(ticket.uploadUrl, file, 'image/jpeg');

    expect(await screen.findByText(ticket.objectName)).toBeInTheDocument();
  });

  it('renders the summary panel from the read endpoints on mount', async () => {
    vi.mocked(api.getSummary).mockResolvedValue({ count: 3, total_cents: 1700 });
    vi.mocked(api.getExpenses).mockResolvedValue([expenseItem]);
    render(<App />);

    expect(await screen.findByText(/3 receipts · \$17\.00 total/i)).toBeInTheDocument();
    expect(await screen.findByText('starbucks')).toBeInTheDocument();
  });

  it('shows a receipt thumbnail using a signed GET url', async () => {
    vi.mocked(api.getSummary).mockResolvedValue({ count: 1, total_cents: 450 });
    vi.mocked(api.getExpenses).mockResolvedValue([expenseItem]);
    render(<App />);

    const img = (await screen.findByAltText(/starbucks receipt/i)) as HTMLImageElement;
    expect(img.src).toContain('signed-get-url');
    expect(api.getReceiptUrl).toHaveBeenCalledWith(expenseItem.source_object);
  });

  it('refreshes the summary after an upload (proves the async pipeline is observed)', async () => {
    render(<App />);
    // baseline: mount loaded the empty summary
    expect(await screen.findByText(/0 receipts · \$0\.00 total/i)).toBeInTheDocument();

    // after upload the pipeline has processed one receipt
    vi.mocked(api.getSummary).mockResolvedValue({ count: 1, total_cents: 450 });
    vi.mocked(api.getExpenses).mockResolvedValue([expenseItem]);

    const { user } = await fillForm();
    await user.click(screen.getByRole('button', { name: /upload/i }));

    expect(await screen.findByText(/1 receipt · \$4\.50 total/i)).toBeInTheDocument();
    // the panel refreshed because upload triggered another read
    await waitFor(() => {
      expect(vi.mocked(api.getSummary).mock.calls.length).toBeGreaterThan(1);
    });
  });

  it('shows an error message when the api rejects', async () => {
    vi.mocked(api.createUploadUrl).mockRejectedValue(new Error('invalid amount'));
    render(<App />);
    const { user } = await fillForm();

    await user.click(screen.getByRole('button', { name: /upload/i }));

    expect(await screen.findByText(/invalid amount/i)).toBeInTheDocument();
    expect(api.putFile).not.toHaveBeenCalled();
  });
});
