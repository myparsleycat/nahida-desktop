package db

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

func TestModClassificationsStore(t *testing.T) {
	t.Parallel()

	client := mustNewTemp(t)
	ctx := context.Background()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	for _, game := range []string{"GI", "HSR"} {
		if err := client.GamePaths.Insert(ctx, GamePathRow{Game: game, ModFolderPath: "C:/" + game}); err != nil {
			t.Fatalf("insert game: %v", err)
		}
	}

	element := ModClassificationRow{ID: "element", Game: "GI", Name: "Element", ItemOrder: 0}
	if err := client.ModClassifications.Save(ctx, element, []ModClassificationGroupRow{
		{ID: "pyro", Name: "Pyro", ItemOrder: 0},
		{ID: "hydro", Name: "Hydro", ItemOrder: 1},
		{ID: "anemo", Name: "Anemo", ItemOrder: 2},
	}); err != nil {
		t.Fatalf("save element: %v", err)
	}
	weapon := ModClassificationRow{ID: "weapon", Game: "GI", Name: "Weapon", ItemOrder: 1}
	if err := client.ModClassifications.Save(ctx, weapon, nil); err != nil {
		t.Fatalf("save weapon: %v", err)
	}
	if err := client.ModClassifications.Save(
		ctx, ModClassificationRow{ID: "path", Game: "HSR", Name: "Path"}, nil,
	); err != nil {
		t.Fatalf("save path: %v", err)
	}

	// Swap two names, drop one group, add one, and try to steal another classification's group ID.
	element.Name = "Elements"
	element.ItemOrder = 99
	if err := client.ModClassifications.Save(ctx, element, []ModClassificationGroupRow{
		{ID: "hydro", Name: "Pyro", ItemOrder: 0},
		{ID: "pyro", Name: "Hydro", ItemOrder: 1},
		{ID: "cryo", Name: "Cryo", ItemOrder: 2},
	}); err != nil {
		t.Fatalf("replace groups: %v", err)
	}
	if err := client.ModClassifications.Save(ctx, weapon, []ModClassificationGroupRow{
		{ID: "cryo", Name: "Sword", ItemOrder: 0},
	}); err != nil {
		t.Fatalf("save foreign group id: %v", err)
	}

	listed, err := client.ModClassifications.ListByGame(ctx, "GI")
	if err != nil || len(listed) != 2 || listed[0].ID != "element" || listed[1].ID != "weapon" {
		t.Fatalf("listed = %+v, err = %v", listed, err)
	}
	if listed[0].Name != "Elements" || listed[0].ItemOrder != 0 || len(listed[1].Groups) != 0 {
		t.Fatalf("listed = %+v", listed)
	}
	groups := listed[0].Groups
	if len(groups) != 3 || groups[0] != (ModClassificationGroupRow{"hydro", "element", "Pyro", 0}) ||
		groups[1] != (ModClassificationGroupRow{"pyro", "element", "Hydro", 1}) ||
		groups[2] != (ModClassificationGroupRow{"cryo", "element", "Cryo", 2}) {
		t.Fatalf("groups = %+v", groups)
	}

	if err := client.ModClassifications.SetActive(ctx, "GI", ptr("element")); err != nil {
		t.Fatalf("activate element: %v", err)
	}
	if err := client.ModClassifications.SetActive(ctx, "HSR", ptr("path")); err != nil {
		t.Fatalf("activate path: %v", err)
	}
	if err := client.ModClassifications.SetActive(ctx, "GI", ptr("weapon")); err != nil {
		t.Fatalf("activate weapon: %v", err)
	}
	listed, err = client.ModClassifications.ListByGame(ctx, "GI")
	if err != nil || listed[0].IsActive || !listed[1].IsActive {
		t.Fatalf("active after switch = %+v, err = %v", listed, err)
	}
	if other, err := client.ModClassifications.FindByID(ctx, "path"); err != nil || other == nil || !other.IsActive {
		t.Fatalf("other game active = %+v, err = %v", other, err)
	}
	if err := client.ModClassifications.SetActive(ctx, "GI", nil); err != nil {
		t.Fatalf("clear active: %v", err)
	}
	if found, err := client.ModClassifications.FindByID(ctx, "weapon"); err != nil || found == nil || found.IsActive {
		t.Fatalf("weapon after clear = %+v, err = %v", found, err)
	}

	if err := client.ModClassifications.Delete(ctx, "weapon"); err != nil {
		t.Fatalf("delete weapon: %v", err)
	}
	if err := client.GamePaths.Delete(ctx, "GI"); err != nil {
		t.Fatalf("delete game: %v", err)
	}
	if listed, err := client.ModClassifications.ListByGame(ctx, "GI"); err != nil || len(listed) != 0 {
		t.Fatalf("after game delete = %+v, err = %v", listed, err)
	}
	var remaining int
	if err := client.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM "mod_classification_groups"`).
		Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("groups after cascade = %d, err = %v", remaining, err)
	}
	if missing, err := client.ModClassifications.FindByID(ctx, "element"); err != nil || missing != nil {
		t.Fatalf("missing = %+v, err = %v", missing, err)
	}
}

func TestModClassificationsStoreConcurrentSnapshot(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name     string
		existing bool
	}{
		{name: "empty game"},
		{name: "existing classification", existing: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			client := mustNewTemp(t)
			ctx := t.Context()
			if err := client.Reconcile(ctx); err != nil {
				t.Fatal(err)
			}
			if err := client.GamePaths.Insert(ctx, GamePathRow{Game: "Game", ModFolderPath: t.TempDir()}); err != nil {
				t.Fatal(err)
			}
			if test.existing {
				if err := client.ModClassifications.Save(ctx,
					ModClassificationRow{ID: "stable", Game: "Game", Name: "Stable"},
					[]ModClassificationGroupRow{{ID: "stable-group", Name: "Stable group"}},
				); err != nil {
					t.Fatal(err)
				}
			}

			const readers = 4
			const iterations = 200
			start := make(chan struct{})
			failures := make(chan error, readers+1)
			var workers sync.WaitGroup
			workers.Add(readers + 1)
			for range readers {
				go func() {
					defer workers.Done()
					<-start
					for range iterations {
						rows, err := client.ModClassifications.ListByGame(ctx, "Game")
						if err != nil {
							failures <- err
							return
						}
						foundStable := false
						for _, row := range rows {
							foundStable = foundStable || row.ID == "stable"
							if len(row.Groups) != 1 || row.Groups[0].ClassificationID != row.ID ||
								row.Groups[0].ID != row.ID+"-group" {
								failures <- fmt.Errorf("inconsistent classification snapshot: %+v", row)
								return
							}
						}
						if foundStable != test.existing {
							failures <- fmt.Errorf("stable classification present = %t, want %t", foundStable, test.existing)
							return
						}
					}
				}()
			}
			go func() {
				defer workers.Done()
				<-start
				for range iterations {
					if err := client.ModClassifications.Save(ctx,
						ModClassificationRow{ID: "changing", Game: "Game", Name: "Changing"},
						[]ModClassificationGroupRow{{ID: "changing-group", Name: "Changing group"}},
					); err != nil {
						failures <- err
						return
					}
					if err := client.ModClassifications.Delete(ctx, "changing"); err != nil {
						failures <- err
						return
					}
				}
			}()
			close(start)
			workers.Wait()
			close(failures)
			for err := range failures {
				t.Error(err)
			}
		})
	}
}
