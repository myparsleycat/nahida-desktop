CREATE TABLE `mod_toggle_persist_preset_items` (
	`preset_id` text NOT NULL,
	`ini_relative_path` text NOT NULL,
	`ini_name` text NOT NULL,
	`section_name` text NOT NULL,
	`variable` text NOT NULL,
	`value` text NOT NULL,
	`item_order` integer NOT NULL,
	PRIMARY KEY(`preset_id`, `ini_relative_path`, `variable`),
	FOREIGN KEY (`preset_id`) REFERENCES `mod_toggle_persist_presets`(`id`) ON UPDATE no action ON DELETE cascade
);
--> statement-breakpoint
CREATE TABLE `mod_toggle_persist_presets` (
	`id` text PRIMARY KEY NOT NULL,
	`game` text NOT NULL,
	`mod_key` text NOT NULL,
	`relative_path` text NOT NULL,
	`group_relative_path` text NOT NULL,
	`folder_name` text NOT NULL,
	`name` text NOT NULL,
	`item_count` integer DEFAULT 0 NOT NULL,
	`created_at` text NOT NULL,
	`updated_at` text NOT NULL,
	FOREIGN KEY (`game`) REFERENCES `game_paths`(`game`) ON UPDATE no action ON DELETE no action
);
--> statement-breakpoint
CREATE UNIQUE INDEX `mod_toggle_persist_presets_game_mod_name_idx` ON `mod_toggle_persist_presets` (`game`,`mod_key`,`name`);