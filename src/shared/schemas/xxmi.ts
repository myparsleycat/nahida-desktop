import { z } from "zod";

const ProxySchema = z.object({
    enable: z.boolean(),
    type: z.string(),
    host: z.string(),
    port: z.string(),
    use_credentials: z.boolean(),
    user: z.string(),
    password: z.string(),
    proxy_dns_via_socks5: z.boolean(),
});

const LauncherConfigSchema = z.object({
    auto_update: z.boolean(),
    pre_release: z.boolean(),
    update_channel: z.string(),
    auto_close: z.boolean(),
    start_timeout: z.number(),
    gui_theme: z.string(),
    theme_mode: z.string(),
    active_importer: z.string(),
    enabled_importers: z.array(z.string()),
    log_level: z.string(),
    config_version: z.string(),
    theme_dev_mode: z.boolean(),
    github_token: z.string(),
    verify_ssl: z.boolean(),
    proxy: ProxySchema,
    credits_shown: z.boolean(),
    locale: z.string(),
});

const PackageItemSchema = z.object({
    latest_version: z.string(),
    skipped_version: z.string(),
    deployed_version: z.string(),
    update_check_time: z.number(),
    latest_release_notes: z.string(),
    deployed_release_notes: z.string(),
});

const PackagesSchema = z.object({
    packages: z.record(z.string(), PackageItemSchema),
});

const BaseMigotoSchema = z.object({
    enforce_rendering: z.boolean(),
    enable_hunting: z.boolean(),
    dump_shaders: z.boolean(),
    mute_warnings: z.boolean(),
    calls_logging: z.boolean(),
    debug_logging: z.boolean(),
    unsafe_mode: z.boolean(),
    unsafe_mode_signature: z.string(),
});

const D3DX_LoaderSchema = z.object({
    Loader: z.object({
        loader: z.string(),
    }),
});

const D3DX_RenderingSchema = z.object({
    Rendering: z.object({
        texture_hash: z.number(),
        track_texture_updates: z.number(),
    }),
});

const D3DX_LoggingToggleSchema = z.object({
    on: z.number(),
    off: z.number(),
});

const D3DX_LoggingCallsSchema = z.object({
    Logging: z.object({
        calls: D3DX_LoggingToggleSchema,
    }),
});

const D3DX_LoggingDebugSchema = z.object({
    Logging: z.object({
        debug: D3DX_LoggingToggleSchema,
    }),
});

const D3DX_LoggingWarningsSchema = z.object({
    Logging: z.object({
        show_warnings: D3DX_LoggingToggleSchema,
    }),
});

const D3DX_HuntingSchema = z.object({
    Hunting: z.object({
        hunting: z.object({
            on: z.number(),
            off: z.number(),
        }),
    }),
});

const D3DX_DumpShadersSchema = z.object({
    Hunting: z.object({
        marking_actions: z.object({
            on: z.string(),
            off: z.string(),
        }),
    }),
});

const D3DXIniSchema = z.object({
    core: D3DX_LoaderSchema,
    enforce_rendering: D3DX_RenderingSchema,
    calls_logging: D3DX_LoggingCallsSchema,
    debug_logging: D3DX_LoggingDebugSchema,
    mute_warnings: D3DX_LoggingWarningsSchema,
    enable_hunting: D3DX_HuntingSchema,
    dump_shaders: D3DX_DumpShadersSchema,
});

const BaseImporterSchema = z.object({
    game_exe_names: z.array(z.string()),
    game_folder_names: z.array(z.string()),
    game_folder_children: z.array(z.string()),
    package_name: z.string(),
    importer_folder: z.string(),
    game_folder: z.string(),
    use_launch_options: z.boolean(),
    overwrite_ini: z.boolean(),
    process_start_method: z.string(),
    xxmi_dll_init_delay: z.number(),
    process_priority: z.string(),
    window_mode: z.string(),
    run_pre_launch_enabled: z.boolean(),
    run_pre_launch: z.string(),
    run_pre_launch_signature: z.string(),
    run_pre_launch_wait: z.boolean(),
    custom_launch_enabled: z.boolean(),
    custom_launch: z.string(),
    custom_launch_signature: z.string(),
    custom_launch_inject_mode: z.string(),
    run_post_load_enabled: z.boolean(),
    run_post_load: z.string(),
    run_post_load_signature: z.string(),
    run_post_load_wait: z.boolean(),
    extra_libraries_enabled: z.boolean(),
    extra_libraries: z.string(),
    extra_libraries_signature: z.string(),
    deployed_migoto_signatures: z.record(z.string(), z.string()),
    shortcut_deployed: z.boolean(),
    d3dx_ini: D3DXIniSchema,
    configure_game: z.boolean(),
    launch_count: z.number(),
    launch_options: z.string(),
});

const GIMI_ImporterSchema = z.object({
    Importer: BaseImporterSchema.extend({
        unlock_fps: z.boolean(),
        unlock_fps_value: z.number().optional(),
        disable_dcr: z.boolean(),
        enable_hdr: z.boolean(),
    }),
    Migoto: BaseMigotoSchema,
});

const SRMI_ImporterSchema = z.object({
    Importer: BaseImporterSchema.extend({
        unlock_fps: z.boolean().optional(),
    }),
    Migoto: BaseMigotoSchema,
});

const WWMI_ImporterSchema = z.object({
    Importer: BaseImporterSchema.extend({
        apply_perf_tweaks: z.boolean(),
        perf_tweaks: z.object({
            SystemSettings: z.record(z.string(), z.union([z.number(), z.string(), z.boolean()])),
        }),
        mesh_lod_distance_scale: z.number(),
        mesh_lod_distance_offset: z.number(),
        texture_streaming_boost: z.number(),
        texture_streaming_min_boost: z.number(),
        texture_streaming_use_all_mips: z.boolean(),
        texture_streaming_pool_size: z.number(),
        texture_streaming_limit_to_vram: z.boolean(),
        texture_streaming_fixed_pool_size: z.boolean(),
        unlock_fps: z.boolean().optional(),
        force_max_lod_bias: z.boolean().optional(),
        disable_wounded_fx: z.boolean().optional(),
        disable_wounded_fx_warned: z.boolean().optional(),
    }),
    Migoto: BaseMigotoSchema,
});

const ZZMI_ImporterSchema = z.object({
    Importer: BaseImporterSchema,
    Migoto: BaseMigotoSchema,
});

const EFMI_ImporterSchema = z.object({
    Importer: BaseImporterSchema,
    Migoto: BaseMigotoSchema,
});

const HIMI_ImporterSchema = z.object({
    Importer: BaseImporterSchema.extend({
        unlock_fps: z.boolean(),
        unlock_fps_value: z.number(),
        disable_dcr: z.boolean(),
        enable_hdr: z.boolean(),
    }),
    Migoto: BaseMigotoSchema,
});

const ImportersSchema = z
    .object({
        GIMI: GIMI_ImporterSchema,
        SRMI: SRMI_ImporterSchema,
        WWMI: WWMI_ImporterSchema,
        ZZMI: ZZMI_ImporterSchema,
        EFMI: EFMI_ImporterSchema,
        HIMI: HIMI_ImporterSchema,
    })
    .catchall(
        z.object({
            Importer: BaseImporterSchema,
            Migoto: BaseMigotoSchema,
        }),
    );

const SecuritySchema = z.object({
    user_signature: z.string(),
});

export const XXMIConfigSchema = z.object({
    Launcher: LauncherConfigSchema,
    Packages: PackagesSchema,
    Importers: ImportersSchema,
    Security: SecuritySchema,
});

export type XXMIConfig = z.infer<typeof XXMIConfigSchema>;
