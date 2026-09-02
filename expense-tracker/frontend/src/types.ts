export interface ReceiptMeta {
  vendor: string;
  amount: number;
  currency: string;
  date: string;
  ext: string;
  contentType: string;
}

export interface UploadTicket {
  uploadUrl: string;
  objectName: string;
  method: string;
  headers: Record<string, string>;
}

/** Running aggregate maintained by the SummaryConsumer (Pub/Sub path). */
export interface Summary {
  count: number;
  total_cents: number;
}

/** One stored expense as returned by GET /expenses (ReceiptUploaded path). */
export interface ExpenseItem {
  id: string;
  vendor: string;
  amount_cents: number;
  currency: string;
  spent_on: string;
  source_object: string;
  created_at: string;
}
