export interface AppStateSchema {
  currentCharPath: string | null;
  isLoggedIn: boolean;
}

export const appStateSchema: AppStateSchema = {
  currentCharPath: null,
  isLoggedIn: false,
};

export interface ModsSchema {
  currentCharPath: string | null;
}

export const modsSchema: ModsSchema = {
  currentCharPath: null,
};

export interface CacheSchema { };

export const cacheSchema: CacheSchema = {};