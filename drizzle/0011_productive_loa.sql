CREATE TABLE `mod_preset_items` (
	`preset_id` text NOT NULL,
	`mod_key` text NOT NULL,
	`relative_path` text NOT NULL,
	`group_relative_path` text NOT NULL,
	`folder_name` text NOT NULL,
	`is_enabled` integer NOT NULL,
	`item_order` integer NOT NULL,
	PRIMARY KEY(`preset_id`, `mod_key`),
	FOREIGN KEY (`preset_id`) REFERENCES `mod_presets`(`id`) ON UPDATE no action ON DELETE cascade
);
--> statement-breakpoint
DROP INDEX `mod_presets_name_unique`;--> statement-breakpoint
ALTER TABLE `mod_presets` ADD `description` text;--> statement-breakpoint
ALTER TABLE `mod_presets` ADD `item_count` integer DEFAULT 0 NOT NULL;--> statement-breakpoint
ALTER TABLE `mod_presets` ADD `created_at` text NOT NULL;--> statement-breakpoint
ALTER TABLE `mod_presets` ADD `updated_at` text NOT NULL;--> statement-breakpoint
ALTER TABLE `mod_presets` ADD `version` integer DEFAULT 1 NOT NULL;--> statement-breakpoint
CREATE UNIQUE INDEX `mod_presets_game_name_idx` ON `mod_presets` (`game`,`name`);--> statement-breakpoint
ALTER TABLE `mod_presets` DROP COLUMN `mods`;