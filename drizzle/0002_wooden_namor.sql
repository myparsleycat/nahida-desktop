PRAGMA foreign_keys=OFF;--> statement-breakpoint
CREATE TABLE `__new_mod_presets` (
	`id` text PRIMARY KEY NOT NULL,
	`game` text NOT NULL,
	`name` text NOT NULL,
	`mods` text NOT NULL,
	FOREIGN KEY (`game`) REFERENCES `game_paths`(`game`) ON UPDATE no action ON DELETE no action
);
--> statement-breakpoint
INSERT INTO `__new_mod_presets`("id", "game", "name", "mods") SELECT "id", "game", "name", "mods" FROM `mod_presets`;--> statement-breakpoint
DROP TABLE `mod_presets`;--> statement-breakpoint
ALTER TABLE `__new_mod_presets` RENAME TO `mod_presets`;--> statement-breakpoint
PRAGMA foreign_keys=ON;--> statement-breakpoint
CREATE UNIQUE INDEX `mod_presets_name_unique` ON `mod_presets` (`name`);