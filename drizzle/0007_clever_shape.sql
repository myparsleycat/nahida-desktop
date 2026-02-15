PRAGMA foreign_keys=OFF;--> statement-breakpoint
CREATE TABLE `__new_fix_tool` (
	`id` text PRIMARY KEY NOT NULL,
	`name` text NOT NULL,
	`source` blob NOT NULL,
	`type` text NOT NULL,
	`size` integer NOT NULL,
	`sha256` text NOT NULL
);
--> statement-breakpoint
INSERT INTO `__new_fix_tool`("id", "name", "source", "type", "size", "sha256") SELECT "id", "name", "source", "type", "size", "sha256" FROM `fix_tool`;--> statement-breakpoint
DROP TABLE `fix_tool`;--> statement-breakpoint
ALTER TABLE `__new_fix_tool` RENAME TO `fix_tool`;--> statement-breakpoint
PRAGMA foreign_keys=ON;--> statement-breakpoint
CREATE UNIQUE INDEX `fix_tool_name_unique` ON `fix_tool` (`name`);