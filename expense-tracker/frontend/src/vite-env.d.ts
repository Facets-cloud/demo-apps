/// <reference types="vite/client" />

interface ImportMetaEnv {
  // Base path for the upload-URL API. Defaults to the same-origin "/api",
  // which nginx reverse-proxies to the CreateUploadURL service in production.
  readonly VITE_API_BASE?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
