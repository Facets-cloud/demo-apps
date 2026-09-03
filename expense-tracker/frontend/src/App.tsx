import { useEffect, useRef, useState, type FormEvent } from 'react';
import { createUploadUrl, putFile, getSummary, getExpenses, getReceiptUrl } from './api';
import type { ReceiptMeta, Summary, ExpenseItem } from './types';
import './styles.css';

// After an upload the async pipeline (GCS finalize → ReceiptUploaded → Pub/Sub →
// SummaryConsumer) takes a few seconds. Poll the read endpoints until the summary
// count advances past its pre-upload baseline, so the result appears on its own.
const POLL_MS = 2000;
const POLL_MAX = 8;

function today(): string {
  return new Date().toISOString().slice(0, 10);
}

function extFromName(name: string): string {
  const dot = name.lastIndexOf('.');
  return dot >= 0 ? name.slice(dot + 1).toLowerCase() : '';
}

function money(cents: number): string {
  return (cents / 100).toFixed(2);
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

  const [summary, setSummary] = useState<Summary | null>(null);
  const [expenses, setExpenses] = useState<ExpenseItem[]>([]);
  const [receiptUrls, setReceiptUrls] = useState<Record<string, string>>({});
  const [polling, setPolling] = useState(false);
  const timerRef = useRef<number | null>(null);

  const loading = status.kind === 'loading';

  // Fetch the current summary + expenses. Best-effort: a failure leaves the
  // panel as-is rather than surfacing an error over the upload flow.
  async function refresh(): Promise<Summary | null> {
    try {
      const [s, xs] = await Promise.all([getSummary(), getExpenses()]);
      if (s) setSummary(s);
      if (xs) setExpenses(xs);
      return s ?? null;
    } catch {
      return null;
    }
  }

  useEffect(() => {
    void refresh();
    return () => {
      if (timerRef.current) window.clearInterval(timerRef.current);
    };
  }, []);

  // Fetch a signed view URL for each receipt we don't have one for yet, so the
  // list can show a thumbnail without the bucket being public.
  useEffect(() => {
    let cancelled = false;
    (async () => {
      for (const x of expenses) {
        if (!x.source_object || receiptUrls[x.source_object]) continue;
        try {
          const url = await getReceiptUrl(x.source_object);
          if (!cancelled) setReceiptUrls((m) => ({ ...m, [x.source_object]: url }));
        } catch {
          // ignore — the row just renders without a thumbnail
        }
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [expenses]); // eslint-disable-line react-hooks/exhaustive-deps

  function startPolling(baseline: number) {
    if (timerRef.current) window.clearInterval(timerRef.current);
    setPolling(true);
    let attempts = 0;
    void refresh();
    timerRef.current = window.setInterval(async () => {
      attempts += 1;
      const s = await refresh();
      const advanced = (s?.count ?? 0) > baseline;
      if (advanced || attempts >= POLL_MAX) {
        if (timerRef.current) window.clearInterval(timerRef.current);
        timerRef.current = null;
        setPolling(false);
      }
    }, POLL_MS);
  }

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
      const baseline = summary?.count ?? 0;
      setStatus({ kind: 'success', objectName: ticket.objectName });
      startPolling(baseline);
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Upload failed';
      setStatus({ kind: 'error', message });
    }
  }

  const count = summary?.count ?? 0;
  const totalCents = summary?.total_cents ?? 0;

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

        <section className="panel panel--summary" aria-label="summary">
          <div className="summary__head">
            <strong>Summary</strong>
            {polling && (
              <span className="summary__spin" role="status">
                processing… updates automatically
              </span>
            )}
          </div>
          <p className="summary__stat">
            {count} receipt{count === 1 ? '' : 's'} · ${money(totalCents)} total
          </p>
          {expenses.length > 0 && (
            <ul className="summary__list">
              {expenses.map((x) => (
                <li key={x.id}>
                  {receiptUrls[x.source_object] ? (
                    <img
                      className="summary__thumb"
                      src={receiptUrls[x.source_object]}
                      alt={`${x.vendor} receipt`}
                    />
                  ) : (
                    <span className="summary__thumb summary__thumb--empty" aria-hidden="true" />
                  )}
                  <span className="summary__vendor">{x.vendor}</span>
                  <span className="summary__amt">
                    {x.currency} {money(x.amount_cents)}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </section>
      </div>
    </main>
  );
}
