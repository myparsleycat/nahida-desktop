CREATE TABLE `toggle_viewer_artifact` (
	`id` text PRIMARY KEY NOT NULL,
	`target_ini_path` text NOT NULL,
	`toggle_txt_path` text NOT NULL,
	`toggle_ini_path` text NOT NULL,
	`toggle_txt_hash` text NOT NULL,
	`toggle_ini_hash` text NOT NULL,
	`updated_at` text NOT NULL
);
--> statement-breakpoint
CREATE UNIQUE INDEX `toggle_viewer_artifact_target_ini_path_unique` ON `toggle_viewer_artifact` (`target_ini_path`);