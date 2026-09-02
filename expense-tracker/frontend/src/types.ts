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
