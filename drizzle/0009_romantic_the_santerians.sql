ALTER TABLE `script` ADD `is_src_zstd` integer DEFAULT false NOT NULL;--> statement-breakpoint
ALTER TABLE `script` ADD `zstd_size` integer DEFAULT NULL;--> statement-breakpoint
ALTER TABLE `script` ADD `zstd_sha256` text DEFAULT NULL;