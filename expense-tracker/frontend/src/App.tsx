import { useState, type FormEvent } from 'react';
import { createUploadUrl, putFile } from './api';
import type { ReceiptMeta } from './types';
import './styles.css';

function today(): string {
  return new Date().toISOString().slice(0, 10);
}

function extFromName(name: string): string {
  const dot = name.lastIndexOf('.');
  return dot >= 0 ? name.slice(dot + 1).toLowerCase() : '';
}

type Status =
  | { kind: 'idle' }
  | { kind: 'loading' }
  | { kind: 'success'; objectName: string }
  | { kind: 'error'; message: string };

export default function App() {
  const [file, setFile] = useState<File | null>(null);
  const [vendor, setVendor] = useState('');
  const [amount, setAmount] = useState('');
  const [currency, setCurrency] = useState('USD');
  const [date, setDate] = useState(today());
  const [status, setStatus] = useState<Status>({ kind: 'idle' });

  const loading = status.kind === 'loading';

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    if (!file) return;

    setStatus({ kind: 'loading' });
    try {
      const contentType = file.type || 'application/octet-stream';
      const meta: ReceiptMeta = {
        vendor,
        amount: Number(amount),
        currency,
        date,
        ext: extFromName(file.name),
        contentType,
      };
      const ticket = await createUploadUrl(meta);
      await putFile(ticket.uploadUrl, file, ticket.headers['Content-Type']);
      setStatus({ kind: 'success', objectName: ticket.objectName });
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Upload failed';
      setStatus({ kind: 'error', message });
    }
  }

  return (
    <main className="app">
      <div className="card">
        <header className="card__header">
          <h1>Receipt Tracker</h1>
          <p className="subtitle">Upload a receipt and log the expense.</p>
        </header>

        <form className="form" onSubmit={handleSubmit}>
          <div className="field">
            <label htmlFor="file">Receipt image</label>
            <input
              id="file"
              type="file"
              accept="image/*"
              onChange={(e) => setFile(e.target.files?.[0] ?? null)}
            />
          </div>

          <div className="field">
            <label htmlFor="vendor">Vendor</label>
            <input
              id="vendor"
              type="text"
              value={vendor}
              placeholder="Starbucks"
              onChange={(e) => setVendor(e.target.value)}
              required
            />
          </div>

          <div className="row">
            <div className="field">
              <label htmlFor="amount">Amount</label>
              <input
                id="amount"
                type="number"
                step="0.01"
                min="0"
                value={amount}
                placeholder="4.50"
                onChange={(e) => setAmount(e.target.value)}
                required
              />
            </div>
            <div className="field field--currency">
              <label htmlFor="currency">Currency</label>
              <input
                id="currency"
                type="text"
                value={currency}
                onChange={(e) => setCurrency(e.target.value.toUpperCase())}
                maxLength={3}
                required
              />
            </div>
          </div>

          <div className="field">
            <label htmlFor="date">Date</label>
            <input
              id="date"
              type="date"
              value={date}
              onChange={(e) => setDate(e.target.value)}
              required
            />
          </div>

          <button type="submit" className="btn" disabled={!file || loading}>
            {loading ? 'Uploading…' : 'Upload receipt'}
          </button>
        </form>

        {status.kind === 'success' && (
          <div className="panel panel--success" role="status">
            <strong>Uploaded ✓</strong>
            <p>Stored as:</p>
            <code className="object-name">{status.objectName}</code>
          </div>
        )}

        {status.kind === 'error' && (
          <div className="panel panel--error" role="alert">
            <strong>Upload failed</strong>
            <p>{status.message}</p>
          </div>
        )}
      </div>
    </main>
  );
}
