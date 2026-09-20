package app

import "testing"

func TestElevatedLifecycleRequestRejectsInvalidatedRead(t *testing.T) {
	t.Parallel()

	t.Run("disable during read", func(t *testing.T) {
		t.Parallel()

		lifecycle := newElevatedLifecycle(nil, nil)
		lifecycle.request(func() (bool, bool) {
			lifecycle.cancelStartup()
			return true, true
		})
		if lifecycle.started {
			t.Fatal("request enqueued a read that a disable invalidated")
		}
	})

	t.Run("shutdown during read", func(t *testing.T) {
		t.Parallel()

		lifecycle := newElevatedLifecycle(nil, nil)
		lifecycle.request(func() (bool, bool) {
			lifecycle.shutdown()
			return true, true
		})
		if lifecycle.started {
			t.Fatal("request enqueued a read that shutdown invalidated")
		}
	})
}
