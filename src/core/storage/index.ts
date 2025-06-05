import {
  InMemoryStorage,
  SchemaBasedStorage,
  type StorageSchema,
  type SubscriptionCallback
} from './storage';

import {
  appStateSchema,
  modsSchema,
  cacheSchema,
  type AppStateSchema,
  type ModsSchema,
  type CacheSchema
} from './schema';

export const appStateStorage = new SchemaBasedStorage(appStateSchema);
export const modsStorage = new SchemaBasedStorage(modsSchema);

export const cacheStorage = new InMemoryStorage<any>();
export const stringStorage = new InMemoryStorage<string>();
export const numberStorage = new InMemoryStorage<number>();

export function createSchemaStorage<T extends StorageSchema>(schema: T) {
  return new SchemaBasedStorage(schema);
}

export {
  InMemoryStorage,
  SchemaBasedStorage,
  type StorageSchema,
  type SubscriptionCallback,
  type AppStateSchema,
  type ModsSchema,
  type CacheSchema
};

export function subscribeMultiple<T extends StorageSchema>(
  storage: SchemaBasedStorage<T>,
  subscriptions: Record<string, SubscriptionCallback<any>>
): () => void {
  const unsubscribeFunctions = Object.entries(subscriptions).map(([key, callback]) =>
    storage.subscribe(key, callback)
  );

  return () => {
    unsubscribeFunctions.forEach(unsubscribe => unsubscribe());
  };
}

export function subscribeWithCondition<T>(
  storage: InMemoryStorage<T> | SchemaBasedStorage<any>,
  key: string,
  callback: SubscriptionCallback<T>,
  condition: (newValue: T, oldValue: T | undefined) => boolean
): () => void {
  return storage.subscribe(key, (newValue, oldValue, key) => {
    if (condition(newValue, oldValue)) {
      callback(newValue, oldValue, key);
    }
  });
}

export function createDebugSubscriber<T extends StorageSchema>(
  storage: SchemaBasedStorage<T>,
  prefix: string = 'DEBUG'
): () => void {
  const keys = storage.keys();
  const unsubscribeFunctions = keys.map(key =>
    storage.subscribe(key, (newValue, oldValue, key) => {
      console.log(`[${prefix}] ${key}: ${JSON.stringify(oldValue)} → ${JSON.stringify(newValue)}`);
    })
  );

  return () => {
    unsubscribeFunctions.forEach(unsubscribe => unsubscribe());
  };
}