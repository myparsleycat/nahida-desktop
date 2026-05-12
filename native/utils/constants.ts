// WaitResult constants - mirrors the native implementation
export const WaitResult = {
  Found: 0,
  NotFound: -100,
  Timeout: -200,
  Terminated: -300,
} as const;

export type WaitResult = (typeof WaitResult)[keyof typeof WaitResult];
