// src/core/storage/storage.ts

export type StorageSchema = Record<string, any>;
export type SubscriptionCallback<T = any> = (newValue: T, oldValue: T | undefined, key: string) => void;

export class InMemoryStorage<T = any> {
  private storage: Map<string, T> = new Map();
  private ttlStorage: Map<string, number> = new Map();
  private subscribers: Map<string, Set<SubscriptionCallback<T>>> = new Map();

  set(key: string, value: T, ttl?: number): void {
    const oldValue = this.storage.get(key);
    this.storage.set(key, value);

    this.notifySubscribers(key, value, oldValue);

    if (ttl) {
      const expirationTime = Date.now() + ttl;
      this.ttlStorage.set(key, expirationTime);

      setTimeout(() => {
        this.delete(key);
      }, ttl);
    }
  }

  get(key: string): T | undefined {
    if (this.isExpired(key)) {
      this.delete(key);
      return undefined;
    }

    return this.storage.get(key);
  }

  has(key: string): boolean {
    if (this.isExpired(key)) {
      this.delete(key);
      return false;
    }

    return this.storage.has(key);
  }

  delete(key: string): boolean {
    const oldValue = this.storage.get(key);
    const deleted = this.storage.delete(key);

    if (deleted) {
      this.notifySubscribers(key, undefined, oldValue);
    }

    this.ttlStorage.delete(key);
    return deleted;
  }

  clear(): void {
    this.storage.forEach((value, key) => {
      this.notifySubscribers(key, undefined, value);
    });

    this.storage.clear();
    this.ttlStorage.clear();
  }

  subscribe(key: string, callback: SubscriptionCallback<T>): () => void {
    if (!this.subscribers.has(key)) {
      this.subscribers.set(key, new Set());
    }

    this.subscribers.get(key)!.add(callback);

    return () => {
      this.unsubscribe(key, callback);
    };
  }

  unsubscribe(key: string, callback: SubscriptionCallback<T>): boolean {
    const keySubscribers = this.subscribers.get(key);
    if (!keySubscribers) {
      return false;
    }

    const removed = keySubscribers.delete(callback);

    if (keySubscribers.size === 0) {
      this.subscribers.delete(key);
    }

    return removed;
  }

  unsubscribeAll(key?: string): void {
    if (key) {
      this.subscribers.delete(key);
    } else {
      this.subscribers.clear();
    }
  }

  getSubscriberCount(key: string): number {
    return this.subscribers.get(key)?.size ?? 0;
  }

  private notifySubscribers(key: string, newValue: T | undefined, oldValue: T | undefined): void {
    const keySubscribers = this.subscribers.get(key);
    if (!keySubscribers || keySubscribers.size === 0) {
      return;
    }

    keySubscribers.forEach(callback => {
      try {
        callback(newValue as T, oldValue, key);
      } catch (error) {
        console.error(`구독 콜백 실행 중 오류 발생 (키: ${key}):`, error);
      }
    });
  }

  size(): number {
    this.cleanupExpired();
    return this.storage.size;
  }

  keys(): string[] {
    this.cleanupExpired();
    return Array.from(this.storage.keys());
  }

  values(): T[] {
    this.cleanupExpired();
    return Array.from(this.storage.values());
  }

  entries(): [string, T][] {
    this.cleanupExpired();
    return Array.from(this.storage.entries());
  }

  getTTL(key: string): number {
    if (!this.storage.has(key)) {
      return -2;
    }

    const expirationTime = this.ttlStorage.get(key);
    if (!expirationTime) {
      return -1;
    }

    const remainingTime = expirationTime - Date.now();
    return remainingTime > 0 ? remainingTime : 0;
  }

  private isExpired(key: string): boolean {
    const expirationTime = this.ttlStorage.get(key);
    if (!expirationTime) {
      return false;
    }

    return Date.now() > expirationTime;
  }

  private cleanupExpired(): void {
    const now = Date.now();
    const expiredKeys: string[] = [];

    this.ttlStorage.forEach((expirationTime, key) => {
      if (now > expirationTime) {
        expiredKeys.push(key);
      }
    });

    expiredKeys.forEach(key => this.delete(key));
  }

  toJSON(): string {
    this.cleanupExpired();
    const obj: Record<string, T> = {};
    this.storage.forEach((value, key) => {
      obj[key] = value;
    });
    return JSON.stringify(obj);
  }

  fromJSON(json: string): void {
    try {
      const obj = JSON.parse(json);
      this.clear();

      Object.entries(obj).forEach(([key, value]) => {
        this.set(key, value as T);
      });
    } catch (error) {
      throw new Error('잘못된 JSON 형식');
    }
  }
}

export class SchemaBasedStorage<TSchema extends StorageSchema> {
  private storage: Map<string, any> = new Map();
  private ttlStorage: Map<string, number> = new Map();
  private requiredKeys: Set<string> = new Set();
  private schema: TSchema;
  private subscribers: Map<string, Set<SubscriptionCallback<any>>> = new Map();

  constructor(schema: TSchema) {
    this.schema = schema;

    Object.keys(schema).forEach(key => {
      this.requiredKeys.add(key);
      this.storage.set(key, schema[key]);
    });
  }

  set<K extends keyof TSchema>(key: K, value: TSchema[K], ttl?: number): void;
  set(key: string, value: any, ttl?: number): void;
  set(key: string, value: any, ttl?: number): void {
    const oldValue = this.storage.get(key);
    this.storage.set(key, value);

    this.notifySubscribers(key, value, oldValue);

    if (ttl && !this.requiredKeys.has(key)) {
      const expirationTime = Date.now() + ttl;
      this.ttlStorage.set(key, expirationTime);

      setTimeout(() => {
        this.delete(key);
      }, ttl);
    }
  }

  get<K extends keyof TSchema>(key: K): TSchema[K];
  get(key: string): any;
  get(key: string): any {
    if (!this.requiredKeys.has(key) && this.isExpired(key)) {
      this.delete(key);
      return undefined;
    }

    return this.storage.get(key);
  }

  has(key: string): boolean {
    if (this.requiredKeys.has(key)) {
      return true;
    }

    if (this.isExpired(key)) {
      this.delete(key);
      return false;
    }

    return this.storage.has(key);
  }

  delete(key: string): boolean {
    const oldValue = this.storage.get(key);

    if (this.requiredKeys.has(key)) {
      const newValue = this.schema[key];
      this.storage.set(key, newValue);
      this.notifySubscribers(key, newValue, oldValue);
      return true;
    }

    const deleted = this.storage.delete(key);
    if (deleted) {
      this.notifySubscribers(key, undefined, oldValue);
    }

    this.ttlStorage.delete(key);
    return deleted;
  }

  clear(): void {
    this.requiredKeys.forEach(key => {
      const oldValue = this.storage.get(key);
      const newValue = this.schema[key];
      this.storage.set(key, newValue);
      this.notifySubscribers(key, newValue, oldValue);
    });

    const optionalKeys = Array.from(this.storage.keys()).filter(key => !this.requiredKeys.has(key));
    optionalKeys.forEach(key => {
      const oldValue = this.storage.get(key);
      this.storage.delete(key);
      this.notifySubscribers(key, undefined, oldValue);
    });

    this.ttlStorage.clear();
  }

  subscribe(key: string, callback: SubscriptionCallback<any>): () => void {
    if (!this.subscribers.has(key)) {
      this.subscribers.set(key, new Set());
    }

    this.subscribers.get(key)!.add(callback);

    return () => {
      this.unsubscribe(key, callback);
    };
  }

  unsubscribe(key: string, callback: SubscriptionCallback<any>): boolean {
    const keySubscribers = this.subscribers.get(key);
    if (!keySubscribers) {
      return false;
    }

    const removed = keySubscribers.delete(callback);

    if (keySubscribers.size === 0) {
      this.subscribers.delete(key);
    }

    return removed;
  }

  unsubscribeAll(key?: string): void {
    if (key) {
      this.subscribers.delete(key);
    } else {
      this.subscribers.clear();
    }
  }

  getSubscriberCount(key: string): number {
    return this.subscribers.get(key)?.size ?? 0;
  }

  private notifySubscribers(key: string, newValue: any, oldValue: any): void {
    const keySubscribers = this.subscribers.get(key);
    if (!keySubscribers || keySubscribers.size === 0) {
      return;
    }

    keySubscribers.forEach(callback => {
      try {
        callback(newValue, oldValue, key);
      } catch (error) {
        console.error(`구독 콜백 실행 중 오류 발생 (키: ${key}):`, error);
      }
    });
  }

  resetRequiredKeys(): void {
    this.requiredKeys.forEach(key => {
      const oldValue = this.storage.get(key);
      const newValue = this.schema[key];
      this.storage.set(key, newValue);
      this.notifySubscribers(key, newValue, oldValue);
    });
  }

  isRequiredKey(key: string): boolean {
    return this.requiredKeys.has(key);
  }

  getSchema(): TSchema {
    return { ...this.schema };
  }

  size(): number {
    this.cleanupExpired();
    return this.storage.size;
  }

  keys(): string[] {
    this.cleanupExpired();
    return Array.from(this.storage.keys());
  }

  values(): any[] {
    this.cleanupExpired();
    return Array.from(this.storage.values());
  }

  entries(): [string, any][] {
    this.cleanupExpired();
    return Array.from(this.storage.entries());
  }

  getTTL(key: string): number {
    if (!this.storage.has(key)) {
      return -2;
    }

    const expirationTime = this.ttlStorage.get(key);
    if (!expirationTime) {
      return -1;
    }

    const remainingTime = expirationTime - Date.now();
    return remainingTime > 0 ? remainingTime : 0;
  }

  private isExpired(key: string): boolean {
    const expirationTime = this.ttlStorage.get(key);
    if (!expirationTime) {
      return false;
    }

    return Date.now() > expirationTime;
  }

  private cleanupExpired(): void {
    const now = Date.now();
    const expiredKeys: string[] = [];

    this.ttlStorage.forEach((expirationTime, key) => {
      if (now > expirationTime && !this.requiredKeys.has(key)) {
        expiredKeys.push(key);
      }
    });

    expiredKeys.forEach(key => this.delete(key));
  }

  toJSON(): string {
    this.cleanupExpired();
    const obj: Record<string, any> = {};
    this.storage.forEach((value, key) => {
      obj[key] = value;
    });
    return JSON.stringify(obj);
  }

  fromJSON(json: string): void {
    try {
      const obj = JSON.parse(json);

      const oldValues = new Map();
      this.storage.forEach((value, key) => {
        oldValues.set(key, value);
      });

      this.resetRequiredKeys();

      const optionalKeys = Array.from(this.storage.keys()).filter(key => !this.requiredKeys.has(key));
      optionalKeys.forEach(key => this.storage.delete(key));

      Object.entries(obj).forEach(([key, value]) => {
        const oldValue = oldValues.get(key);
        this.storage.set(key, value);
        this.notifySubscribers(key, value, oldValue);
      });
    } catch (error) {
      throw new Error('잘못된 JSON 형식');
    }
  }
}