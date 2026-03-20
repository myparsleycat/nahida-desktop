PRAGMA foreign_keys=OFF;--> statement-breakpoint
CREATE TABLE `__new_mod_presets` (
	`id` text PRIMARY KEY NOT NULL,
	`game` text NOT NULL,
	`name` text NOT NULL,
	`description` text,
	`item_count` integer DEFAULT 0 NOT NULL,
	`created_at` text NOT NULL,
	`updated_at` text NOT NULL,
	`version` integer DEFAULT 1 NOT NULL,
	FOREIGN KEY (`game`) REFERENCES `game_paths`(`game`) ON UPDATE no action ON DELETE cascade
);
--> statement-breakpoint
INSERT INTO `__new_mod_presets`("id", "game", "name", "description", "item_count", "created_at", "updated_at", "version") SELECT "id", "game", "name", "description", "item_count", "created_at", "updated_at", "version" FROM `mod_presets`;--> statement-breakpoint
DROP TABLE `mod_presets`;--> statement-breakpoint
ALTER TABLE `__new_mod_presets` RENAME TO `mod_presets`;--> statement-breakpoint
PRAGMA foreign_keys=ON;--> statement-breakpoint
CREATE UNIQUE INDEX `mod_presets_game_name_idx` ON `mod_presets` (`game`,`name`);