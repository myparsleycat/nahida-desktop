ALTER TABLE `fix_tool` RENAME TO `script`;--> statement-breakpoint
ALTER TABLE `fix_tool_preset` RENAME TO `script_preset`;--> statement-breakpoint
ALTER TABLE `fix_tool_preset_item` RENAME TO `script_preset_item`;--> statement-breakpoint
ALTER TABLE `script_preset_item` RENAME COLUMN "tool_id" TO "script_id";--> statement-breakpoint
DROP INDEX `fix_tool_name_unique`;--> statement-breakpoint
CREATE UNIQUE INDEX `script_name_unique` ON `script` (`name`);--> statement-breakpoint
DROP INDEX `fix_tool_preset_name_unique`;--> statement-breakpoint
CREATE UNIQUE INDEX `script_preset_name_unique` ON `script_preset` (`name`);--> statement-breakpoint
PRAGMA foreign_keys=OFF;--> statement-breakpoint
CREATE TABLE `__new_script_preset_item` (
	`preset_id` text NOT NULL,
	`script_id` text NOT NULL,
	`order` integer NOT NULL,
	PRIMARY KEY(`preset_id`, `script_id`),
	FOREIGN KEY (`preset_id`) REFERENCES `script_preset`(`id`) ON UPDATE no action ON DELETE cascade,
	FOREIGN KEY (`script_id`) REFERENCES `script`(`id`) ON UPDATE no action ON DELETE cascade
);
--> statement-breakpoint
INSERT INTO `__new_script_preset_item`("preset_id", "script_id", "order") SELECT "preset_id", "script_id", "order" FROM `script_preset_item`;--> statement-breakpoint
DROP TABLE `script_preset_item`;--> statement-breakpoint
ALTER TABLE `__new_script_preset_item` RENAME TO `script_preset_item`;--> statement-breakpoint
PRAGMA foreign_keys=ON;