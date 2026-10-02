package mod

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"nahida.live/desktop/internal/mod/namespace"
)

func (c *namespaceIsolationCoordinator) reconcile(ctx context.Context, key string, launch bool) (returnErr error) {
	c.mu.Lock()
	paused := c.paused
	c.mu.Unlock()
	if paused {
		return errors.New("NAMESPACE_ISOLATION_PAUSED")
	}
	c.reconcileMu.Lock()
	defer c.reconcileMu.Unlock()
	ctx, cancel := context.WithCancel(ctx)
	c.mu.Lock()
	if c.paused {
		c.mu.Unlock()
		cancel()
		return errors.New("NAMESPACE_ISOLATION_PAUSED")
	}
	c.activeCancel = cancel
	c.mu.Unlock()
	defer func() { cancel(); c.mu.Lock(); c.activeCancel = nil; c.mu.Unlock() }()
	if err := ctx.Err(); err != nil {
		return err
	}
	previous := c.owner.GetNamespaceIsolationState().Conflicts
	c.publish(true, previous)
	conflicts := []NamespaceIsolationConflict{}
	defer func() {
		// An aborted pass has only a partial list; publishing it would clear real conflicts.
		if returnErr != nil {
			conflicts = previous
		}
		c.publish(false, conflicts)
	}()
	inventories, err := c.inventories(ctx, key)
	if err != nil {
		return err
	}
	if key != "" {
		for _, old := range previous {
			if !strings.EqualFold(old.ImporterKey, key) {
				conflicts = append(conflicts, old)
			}
		}
	}
	roots := []string{}
	for _, inventory := range inventories {
		roots = append(roots, inventory.roots...)
		roots = append(roots, inventory.importer.ImporterFolder)
	}
	c.mu.Lock()
	watchEnabled := c.watchEnabled
	c.mu.Unlock()
	if !launch && watchEnabled {
		if err := c.replaceWatcher(namespacePhysicalPaths(roots)); err != nil {
			return err
		}
	}
	c.mu.Lock()
	hooks := c.hooks
	c.mu.Unlock()
	auto := false
	if setting, ok := c.owner.settings.(interface {
		GetPersistToggles(context.Context) (bool, error)
	}); ok {
		auto, err = setting.GetPersistToggles(ctx)
		if err != nil {
			return err
		}
	}
	for _, inventory := range inventories {
		if key != "" && !strings.EqualFold(key, inventory.importer.Key) {
			continue
		}
		var stopped error
		if hooks.CheckStopped == nil {
			stopped = errors.New("game process guard is unavailable")
		} else {
			stopped = hooks.CheckStopped(ctx, inventory.importer.Key)
		}
		pending := []string{}
		for _, modPath := range inventory.mods {
			unfinished, err := namespace.HasIncompleteTransactions(modPath)
			if unfinished || err != nil {
				pending = append(pending, modPath)
			}
		}
		if len(pending) > 0 {
			recoveryID := namespaceConflictID(inventory.importer.Key, "recovery", strings.Join(pending, "\x00"))
			recoveryHash := namespaceInventoryHash([]namespaceIsolationInventory{inventory}, inventory.importer.Key)
			recoveryEpoch := c.failureEpoch.Load()
			if cached, ok := c.failures[recoveryID]; ok && stopped == nil && cached.hash == recoveryHash &&
				cached.epoch == recoveryEpoch {
				conflicts = append(conflicts, cached.conflict)
				continue
			}
			recoveryErr := stopped
			if stopped == nil && len(inventory.issues) == 0 {
				recoveryErr = c.mutate(ctx, inventory.importer.Key, launch, hooks, func() error {
					if err := hooks.CheckStopped(ctx, inventory.importer.Key); err != nil {
						return err
					}
					if err := c.stabilize(ctx, inventory); err != nil {
						return err
					}
					if err := hooks.CheckStopped(ctx, inventory.importer.Key); err != nil {
						return err
					}
					_, err := namespace.RecoverVerified(ctx, inventory.mods, func() error {
						if err := ctx.Err(); err != nil {
							return err
						}
						return hooks.CheckStopped(ctx, inventory.importer.Key)
					})
					return err
				})
			}
			if recoveryErr == nil && len(inventory.issues) > 0 {
				recoveryErr = errors.New("transaction recovery requires a complete importer inventory")
			}
			if recoveryErr != nil {
				status := "recovery_required"
				if stopped != nil {
					status = "waiting_for_game_exit"
				}
				conflicts = append(conflicts, NamespaceIsolationConflict{
					ID:          recoveryID,
					ImporterKey: inventory.importer.Key, ModPaths: pending, INIPaths: []string{},
					Status: status, Reason: "unresolved_transaction", Detail: recoveryErr.Error(),
				})
				if stopped == nil && ctx.Err() == nil &&
					(namespacePermanentFailure(recoveryErr) || errors.Is(recoveryErr, namespace.ErrRecovery) || errors.Is(recoveryErr, namespace.ErrJournal)) {
					c.failures[recoveryID] = namespaceIsolationFailure{
						hash:     recoveryHash,
						epoch:    recoveryEpoch,
						conflict: conflicts[len(conflicts)-1],
					}
				}
				continue
			}
			delete(c.failures, recoveryID)
			// Recovery changes bytes. Never build a rewrite from pre-recovery snapshots.
			fresh, err := c.inventories(ctx, inventory.importer.Key)
			if err != nil {
				return err
			}
			for _, candidate := range fresh {
				if candidate.importer.Key == inventory.importer.Key {
					inventory = candidate
					break
				}
			}
		}
		refreshConflicts := []NamespaceIsolationConflict{}

		// Refresh stale manifests only for noncolliding physical mods. A user
		// namespace edit changes metadata ownership, never the INI declaration.
		if auto && stopped == nil && len(inventory.issues) == 0 {
			colliding := map[string]bool{}
			for _, plan := range analyzeNamespaceIsolation(inventory) {
				for _, path := range plan.mods {
					colliding[path] = true
				}
			}
			for _, path := range inventory.mods {
				if colliding[path] {
					continue
				}
				changes, refreshErr := namespaceManifestRefresh(path, inventory.files)
				if refreshErr != nil {
					refreshConflicts = append(refreshConflicts, NamespaceIsolationConflict{
						ID: namespaceConflictID(
							inventory.importer.Key,
							"invalid_metadata",
							path,
						),
						ImporterKey: inventory.importer.Key,
						ModPaths:    []string{},
						INIPaths:    []string{path},
						Status:      "needs_review",
						Reason:      "invalid_metadata",
						Detail:      refreshErr.Error(),
					})
					continue
				}
				if len(changes) == 0 {
					continue
				}
				refreshErr = c.mutate(ctx, inventory.importer.Key, launch, hooks, func() error {
					if err := hooks.CheckStopped(ctx, inventory.importer.Key); err != nil {
						return err
					}
					if err := c.stabilize(ctx, inventory); err != nil {
						return err
					}
					return namespace.Apply(
						ctx,
						changes,
						func() error { return hooks.CheckStopped(ctx, inventory.importer.Key) },
					)
				})
				if refreshErr != nil {
					refreshConflicts = append(refreshConflicts, NamespaceIsolationConflict{
						ID: namespaceConflictID(
							inventory.importer.Key,
							"metadata_refresh_failed",
							path,
						),
						ImporterKey: inventory.importer.Key,
						ModPaths:    []string{},
						INIPaths:    []string{path},
						Status:      "needs_review",
						Reason:      "metadata_refresh_failed",
						Detail:      refreshErr.Error(),
					})
				}
			}
		}
		conflicts = append(conflicts, inventory.issues...)
		conflicts = append(conflicts, refreshConflicts...)
		plans := analyzeNamespaceIsolation(inventory)
		for _, plan := range plans {
			conflict := plan.conflict
			if conflict.Reason != "namespace_collision" {
				conflicts = append(conflicts, conflict)
				continue
			}
			if !auto {
				conflict.Reason = "automatic_isolation_disabled"
				conflict.Detail = "Toggle persistence is disabled; transaction recovery remains enabled"
				conflicts = append(conflicts, conflict)
				continue
			}
			if stopped != nil {
				conflict.Status = "waiting_for_game_exit"
				conflict.Reason = "game_not_stopped"
				conflict.Detail = stopped.Error()
				conflicts = append(conflicts, conflict)
				continue
			}
			baselineHash := namespaceInventoryHash([]namespaceIsolationInventory{inventory}, inventory.importer.Key)
			epoch := c.failureEpoch.Load()
			if failed, ok := c.failures[conflict.ID]; ok && failed.hash == baselineHash && failed.epoch == epoch {
				conflicts = append(conflicts, failed.conflict)
				continue
			}
			err := c.mutate(ctx, inventory.importer.Key, launch, hooks, func() error {
				if err := hooks.CheckStopped(ctx, inventory.importer.Key); err != nil {
					return err
				}
				before, err := c.inventories(ctx, inventory.importer.Key)
				if err != nil {
					return err
				}
				if namespaceInventoryHash(
					before,
					inventory.importer.Key,
				) != namespaceInventoryHash(
					[]namespaceIsolationInventory{inventory},
					inventory.importer.Key,
				) {
					return errors.New("namespace inventory changed before stabilization")
				}
				timer := time.NewTimer(c.settle)
				defer timer.Stop()
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-timer.C:
				}
				after, err := c.inventories(ctx, inventory.importer.Key)
				if err != nil {
					return err
				}
				baseline := namespaceInventoryHash(before, inventory.importer.Key)
				if baseline != namespaceInventoryHash(after, inventory.importer.Key) {
					return errors.New("namespace inventory or user INI is still changing")
				}
				if err := c.requireAutoEnabled(ctx); err != nil {
					return err
				}
				changes, err := namespaceChanges(plan)
				if err != nil {
					return err
				}
				identities, err := namespaceParticipantIdentities(inventory, plan)
				if err != nil {
					return err
				}
				validate := func() error {
					if err := c.requireAutoEnabled(ctx); err != nil {
						return err
					}
					if err := hooks.CheckStopped(ctx, inventory.importer.Key); err != nil {
						return err
					}
					current, err := c.inventories(ctx, inventory.importer.Key)
					if err != nil {
						return err
					}
					if err := validateNamespaceParticipants(current, inventory, plan, identities); err != nil {
						return err
					}
					if baseline != namespaceInventoryHash(current, inventory.importer.Key) {
						return errors.New("namespace inventory changed before write")
					}
					return nil
				}
				verify := func() error {
					if err := ctx.Err(); err != nil {
						return err
					}
					if err := hooks.CheckStopped(ctx, inventory.importer.Key); err != nil {
						return err
					}
					if err := c.requireAutoEnabled(ctx); err != nil {
						return err
					}
					current, err := c.inventories(ctx, inventory.importer.Key)
					if err != nil {
						return err
					}
					if err := validateNamespaceParticipants(current, inventory, plan, identities); err != nil {
						return err
					}
					for _, candidate := range current {
						if candidate.importer.Key != inventory.importer.Key {
							continue
						}
						if len(candidate.issues) > 0 {
							return errors.New("post-apply inventory is incomplete")
						}
						for _, remaining := range analyzeNamespaceIsolation(candidate) {
							if slices.ContainsFunc(
								remaining.mods,
								func(path string) bool { return slices.Contains(plan.mods, path) },
							) {
								return errors.New("post-apply persisted namespace collision remains")
							}
						}
					}
					return c.requireAutoEnabled(ctx)
				}
				if err := namespace.ApplyVerified(ctx, changes, validate, verify); err != nil {
					return err
				}
				c.mu.Lock()
				for _, change := range changes {
					c.self[strings.ToLower(filepath.Join(change.ModPath, change.RelativePath))] = slices.Clone(
						change.After,
					)
				}
				c.mu.Unlock()
				return nil
			})
			if err != nil {
				conflict.Status = "failed"
				conflict.Reason = "mutation_deferred"
				conflict.Detail = err.Error()
				if strings.Contains(err.Error(), "XXMI_GAME_RUNNING") {
					conflict.Status = "waiting_for_game_exit"
					conflict.Reason = "game_not_stopped"
				}
				// Failed Apply can leave durable prepared journals. Persist and launch
				// consumers must treat this as unresolved, even if no byte was replaced.
				for _, modPath := range plan.mods {
					pending, checkErr := namespace.HasIncompleteTransactions(modPath)
					if pending || checkErr != nil {
						conflict.Reason = "unresolved_transaction"
						if conflict.Status != "waiting_for_game_exit" {
							conflict.Status = "recovery_required"
						}
						break
					}
				}
				if conflict.Status == "failed" && conflict.Reason != "unresolved_transaction" &&
					namespacePermanentFailure(err) &&
					ctx.Err() == nil {
					c.failures[conflict.ID] = namespaceIsolationFailure{
						hash:     baselineHash,
						epoch:    epoch,
						conflict: conflict,
					}
				}
			} else {
				delete(c.failures, conflict.ID)
				c.logIsolationSuccess(conflict)
				// Other plans must compare against the new full importer snapshot.
				fresh, refreshErr := c.inventories(ctx, inventory.importer.Key)
				if refreshErr != nil {
					return refreshErr
				}
				for _, candidate := range fresh {
					if candidate.importer.Key == inventory.importer.Key {
						inventory = candidate
						break
					}
				}
				continue
			}
			conflicts = append(conflicts, conflict)
		}
	}
	slices.SortFunc(conflicts, func(a, b NamespaceIsolationConflict) int { return strings.Compare(a.ID, b.ID) })

	// A walk cancelled midway is recorded as an inventory issue rather than an
	// error, so a cancelled pass must still count as aborted.
	return ctx.Err()
}

func namespaceDirectoryIdentity(path string) (os.FileInfo, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, statErr := file.Stat()
	if err := errors.Join(statErr, file.Close()); err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("namespace participant is not a directory: %s", path)
	}
	return info, nil
}

func namespaceParticipantIdentities(
	inventory namespaceIsolationInventory,
	plan namespaceIsolationPlan,
) (map[string]os.FileInfo, error) {
	identities := map[string]os.FileInfo{}
	paths := append(slices.Clone(plan.mods), inventory.importer.ImporterFolder)
	paths = append(paths, inventory.roots...)
	for _, path := range paths {
		info, err := namespaceDirectoryIdentity(path)
		if err != nil {
			return nil, err
		}
		identities[path] = info
	}
	return identities, nil
}

func validateNamespaceParticipants(
	current []namespaceIsolationInventory,
	before namespaceIsolationInventory,
	plan namespaceIsolationPlan,
	identities map[string]os.FileInfo,
) error {
	var matching *namespaceIsolationInventory
	for index := range current {
		if strings.EqualFold(current[index].importer.Key, before.importer.Key) {
			matching = &current[index]
			break
		}
	}
	if matching == nil {
		return errors.New("post-apply importer is absent")
	}
	if !samePath(matching.importer.ImporterFolder, before.importer.ImporterFolder) {
		return errors.New("post-apply importer root changed")
	}
	for _, root := range before.roots {
		if !slices.ContainsFunc(matching.roots, func(path string) bool { return samePath(path, root) }) {
			return fmt.Errorf("post-apply trusted root is absent: %s", root)
		}
	}
	for _, path := range plan.mods {
		if !slices.ContainsFunc(matching.mods, func(candidate string) bool { return samePath(path, candidate) }) {
			return fmt.Errorf("post-apply participant is absent: %s", path)
		}
	}
	for path, identity := range identities {
		currentIdentity, err := namespaceDirectoryIdentity(path)
		if err != nil {
			return fmt.Errorf("validate namespace root %s: %w", path, err)
		}
		if !os.SameFile(identity, currentIdentity) {
			return fmt.Errorf("namespace physical root changed: %s", path)
		}
	}
	return nil
}

func namespacePermanentFailure(err error) bool {
	message := strings.ToLower(err.Error())
	return errors.Is(err, os.ErrPermission) || errors.Is(err, os.ErrNotExist) ||
		strings.Contains(
			message,
			"access is denied",
		) || strings.Contains(message, "read-only") || strings.Contains(message, "readonly")
}

func (c *namespaceIsolationCoordinator) mutate(
	ctx context.Context,
	key string,
	launch bool,
	hooks NamespaceIsolationHooks,
	change func() error,
) error {
	apply := func() (err error) {
		c.opMu.Lock()
		defer c.opMu.Unlock()

		// A queued writer blocks every new reader, so Lock would stall all mod
		// operations behind one long extraction or merge. TryLock never queues,
		// and the wait stays cancellable when the coordinator stops.
		retry := time.NewTicker(25 * time.Millisecond)
		defer retry.Stop()
		for !c.owner.operationMu.TryLock() {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-retry.C:
			}
		}
		defer c.owner.operationMu.Unlock()
		if err := ctx.Err(); err != nil {
			return err
		}

		if hooks.SuspendPersist == nil {
			return errors.New("persist watcher suspension is unavailable")
		}
		resume, err := hooks.SuspendPersist(ctx)
		if err != nil {
			return err
		}
		if resume == nil {
			return errors.New("persist suspension returned no resume function")
		}
		defer func() { err = errors.Join(err, resume()) }()
		return change()
	}
	if launch {
		return apply()
	}
	if hooks.WithMutation == nil {
		return errors.New("importer mutation gate is unavailable")
	}
	return hooks.WithMutation(ctx, key, apply)
}

func namespaceInventoryHash(inventories []namespaceIsolationInventory, key string) string {
	hash := sha256.New()
	for _, inventory := range inventories {
		if !strings.EqualFold(inventory.importer.Key, key) {
			continue
		}
		for _, path := range inventory.mods {
			_, _ = fmt.Fprintf(hash, "mod:%s\x00", path)
		}
		for _, issue := range inventory.issues {
			_, _ = fmt.Fprintf(hash, "issue:%s:%s\x00", issue.Reason, issue.Detail)
		}
		for _, file := range inventory.files {
			_, _ = fmt.Fprintf(
				hash,
				"ini:%s:%s:%d:%d\x00",
				file.path,
				file.modPath,
				file.info.Size(),
				file.info.ModTime().UnixNano(),
			)
			_, _ = hash.Write(file.content)
		}
		// User settings may be absent initially; creation during game exit must
		// invalidate the snapshot just as an edit does.
		for _, name := range []string{"d3dx_user.ini", "d3dx.ini"} {
			snapshot := inventory.userSnapshots[name]
			content := snapshot.content
			_, _ = fmt.Fprintf(hash, "user:%s:exists:%t\x00", name, content != nil)
			_, _ = hash.Write(content)
			if snapshot.info != nil {
				_, _ = fmt.Fprintf(hash, "%d:%d", snapshot.info.Size(), snapshot.info.ModTime().UnixNano())
			}
		}
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func (c *namespaceIsolationCoordinator) stabilize(ctx context.Context, inventory namespaceIsolationInventory) error {
	baseline := namespaceInventoryHash([]namespaceIsolationInventory{inventory}, inventory.importer.Key)
	timer := time.NewTimer(c.settle)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
	}
	current, err := c.inventories(ctx, inventory.importer.Key)
	if err != nil {
		return err
	}
	if baseline != namespaceInventoryHash(current, inventory.importer.Key) {
		return errors.New("namespace inventory or user INI is still changing")
	}
	return nil
}

func (c *namespaceIsolationCoordinator) requireAutoEnabled(ctx context.Context) error {
	setting, ok := c.owner.settings.(interface {
		GetPersistToggles(context.Context) (bool, error)
	})
	if !ok {
		return errors.New("automatic isolation setting is unavailable")
	}
	enabled, err := setting.GetPersistToggles(ctx)
	if err != nil {
		return err
	}
	if !enabled {
		return errors.New("automatic namespace isolation was disabled")
	}
	return nil
}
