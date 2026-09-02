/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_UPLOAD_URL_ENDPOINT: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
