CREATE TABLE `game_paths` (
	`game` text PRIMARY KEY NOT NULL,
	`modFolderPath` text NOT NULL
);
--> statement-breakpoint
CREATE TABLE `mod_presets` (
	`id` text PRIMARY KEY NOT NULL,
	`game` text NOT NULL,
	`name` text NOT NULL,
	`mods` text NOT NULL
);
