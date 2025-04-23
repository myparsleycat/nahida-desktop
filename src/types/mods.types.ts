
export interface DirectChildren {
  path: string;
  name: string;
  hasIni: boolean;
  previewB64: string | null;
}

export interface getDirectChildrenOptions {
  dirOnly?: boolean;
}