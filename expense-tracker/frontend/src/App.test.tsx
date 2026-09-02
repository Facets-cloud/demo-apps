import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import App from './App';
import * as api from './api';
import type { UploadTicket } from './types';

vi.mock('./api');

const ticket: UploadTicket = {
  uploadUrl: 'https://storage.googleapis.com/signed-put-url',
  objectName: 'receipts/2026-09-01_starbucks_4.50_a1b2c3.jpg',
  method: 'PUT',
  headers: { 'Content-Type': 'image/jpeg' },
};

beforeEach(() => {
  vi.mocked(api.createUploadUrl).mockResolvedValue(ticket);
  vi.mocked(api.putFile).mockResolvedValue(undefined);
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

  it('shows an error message when the api rejects', async () => {
    vi.mocked(api.createUploadUrl).mockRejectedValue(new Error('invalid amount'));
    render(<App />);
    const { user } = await fillForm();

    await user.click(screen.getByRole('button', { name: /upload/i }));

    expect(await screen.findByText(/invalid amount/i)).toBeInTheDocument();
    expect(api.putFile).not.toHaveBeenCalled();
  });
});
