
type Subscriber<T> = (value: T) => void;
type Unsubscriber = () => void;
type Updater<T> = (value: T) => T;

interface WritableStore<T> {
  subscribe: (subscriber: Subscriber<T>) => Unsubscriber;
  set: (value: T) => void;
  update: (updater: Updater<T>) => void;
}

interface StoreOptions<T> {
  onUpdate?: (newValue: T, oldValue: T) => void;
}

export function writable<T>(
  initialValue: T,
  options?: StoreOptions<T>
): WritableStore<T> {
  let value = initialValue;
  const subscribers = new Set<Subscriber<T>>();
  const { onUpdate } = options || {};

  const subscribe = (subscriber: Subscriber<T>): Unsubscriber => {
    subscribers.add(subscriber);
    subscriber(value);

    return () => {
      subscribers.delete(subscriber);
    };
  };

  const set = (newValue: T): void => {
    const oldValue = value;
    value = newValue;

    if (onUpdate && oldValue !== newValue) {
      onUpdate(newValue, oldValue);
    }

    subscribers.forEach(subscriber => subscriber(newValue));
  };

  const update = (updater: Updater<T>): void => {
    set(updater(value));
  };

  return {
    subscribe,
    set,
    update
  };
}