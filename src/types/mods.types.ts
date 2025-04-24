
export interface DirectChildren {
  path: string;
  name: string;
  hasIni: boolean;
  preview: {
    path: string;
    base64: string | null;
  } | null
}

export interface getDirectChildrenOptions {
  dirOnly?: boolean;
}