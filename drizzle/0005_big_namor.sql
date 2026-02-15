CREATE TABLE `fix_tool` (
	`id` text PRIMARY KEY NOT NULL,
	`name` text NOT NULL,
	`source` text NOT NULL,
	`type` text NOT NULL,
	`size` integer NOT NULL
);
--> statement-breakpoint
CREATE UNIQUE INDEX `fix_tool_name_unique` ON `fix_tool` (`name`);--> statement-breakpoint
CREATE TABLE `fix_tool_preset` (
	`id` text PRIMARY KEY NOT NULL,
	`name` text NOT NULL
);
--> statement-breakpoint
CREATE UNIQUE INDEX `fix_tool_preset_name_unique` ON `fix_tool_preset` (`name`);--> statement-breakpoint
CREATE TABLE `fix_tool_preset_item` (
	`preset_id` text NOT NULL,
	`tool_id` text NOT NULL,
	`order` integer NOT NULL,
	PRIMARY KEY(`preset_id`, `tool_id`),
	FOREIGN KEY (`preset_id`) REFERENCES `fix_tool_preset`(`id`) ON UPDATE no action ON DELETE cascade,
	FOREIGN KEY (`tool_id`) REFERENCES `fix_tool`(`id`) ON UPDATE no action ON DELETE cascade
);
