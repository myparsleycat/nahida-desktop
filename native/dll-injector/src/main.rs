use std::ffi::OsStr;
use std::os::windows::ffi::OsStrExt;
use std::path::Path;
use std::process::Command;
use std::thread;
use std::time::{Duration, Instant};

use anyhow::{anyhow, Context, Result};
use clap::{Parser, Subcommand};
use libloading::{Library, Symbol as LibSymbol};
use std::os::windows::process::CommandExt;
use sysinfo::{ProcessRefreshKind, RefreshKind, System};
use windows::core::PCWSTR;
use windows::Win32::Foundation::HANDLE;
use windows::Win32::UI::WindowsAndMessaging::HHOOK;

fn to_wstring(str: &str) -> Vec<u16> {
    OsStr::new(str).encode_wide().chain(Some(0)).collect()
}

type HookLibraryFn = unsafe extern "C" fn(PCWSTR, *mut HHOOK, *mut HANDLE) -> i32;
type WaitForInjectionFn = unsafe extern "C" fn(PCWSTR, PCWSTR, i32) -> i32;
type UnhookLibraryFn = unsafe extern "C" fn(*mut HHOOK, *mut HANDLE) -> i32;
type StartProcessFn = unsafe extern "C" fn(PCWSTR, PCWSTR, PCWSTR) -> i32;

fn helper_start_process(
    lib: &Library,
    exe_path: &str,
    work_dir: &str,
    start_args: &str,
) -> Result<()> {
    let w_exe_path = to_wstring(exe_path);
    let w_work_dir = to_wstring(work_dir);
    let w_start_args = to_wstring(start_args);

    let start_process_fn: LibSymbol<StartProcessFn> = unsafe {
        lib.get(b"StartProcess")
            .context("Failed to get StartProcess")?
    };

    let result = unsafe {
        start_process_fn(
            PCWSTR(w_exe_path.as_ptr()),
            PCWSTR(w_work_dir.as_ptr()),
            PCWSTR(w_start_args.as_ptr()),
        )
    };

    if result != 0 {
        let error_text = match result {
            0 => "The operating system is out of memory/resources",
            2 => "File not found",
            3 => "Path not found",
            5 => "Access denied",
            11 => ".exe file is invalid or not a Win32 app",
            26 => "Sharing violation",
            31 => "No application is associated with the file",
            32 => "File association is incomplete",
            _ => "Unknown ShellExecute error",
        };
        return Err(anyhow!(
            "Failed to start {}: {} code {}",
            exe_path,
            error_text,
            result
        ));
    }

    Ok(())
}

fn helper_inject_libraries(
    dll_paths: &[String],
    process_name: Option<&str>,
    pid: Option<u32>,
    timeout: i32,
) -> Result<i32> {
    if process_name.is_none() && pid.is_none() {
        return Err(anyhow!("process_name or pid must be specified"));
    }

    let t = timeout;
    let time_start = Instant::now();
    let mut sys = System::new();
    let refresh_kind = RefreshKind::new().with_processes(ProcessRefreshKind::new());

    loop {
        if t != -1 && time_start.elapsed().as_secs_f64() >= t as f64 {
            return Err(anyhow!("Timeout waiting for process"));
        }

        sys.refresh_specifics(refresh_kind);

        for (sys_pid, process) in sys.processes() {
            let proc_name = process.name();

            let match_name = if let Some(name) = process_name {
                proc_name == name
            } else {
                false
            };

            let current_pid_u32: u32 = sys_pid.as_u32();

            let match_pid = if let Some(target_pid) = pid {
                current_pid_u32 == target_pid
            } else {
                false
            };

            if match_name || match_pid {
                for dll_path in dll_paths {
                    if let Err(e) = inject(current_pid_u32, dll_path) {
                        return Err(anyhow!(
                            "Failed to inject extra library {}:\n{}!\nPlease check Advanced Settings -> Inject Libraries.",
                            dll_path, e
                        ));
                    }
                }
                return Ok(current_pid_u32 as i32);
            }
        }

        thread::sleep(Duration::from_millis(100));
    }
}

mod injector_util;
use injector_util::Injector;

fn inject(pid: u32, dll_path: &str) -> std::result::Result<(), String> {
    let injector =
        Injector::attach(pid).map_err(|e| format!("Failed to attach injector: {}", e))?;
    injector
        .inject(dll_path)
        .map_err(|e| format!("Failed to inject DLL: {}", e))?;
    Ok(())
}

#[derive(Parser)]
#[command(name = "dll-injector", about = "Dll Injector CLI")]
struct Cli {
    #[command(subcommand)]
    command: Commands,
}

#[derive(Subcommand)]
enum Commands {
    StartProcess {
        #[arg(long)]
        injector_lib_path: String,
        #[arg(long)]
        exe_path: String,
        #[arg(long)]
        work_dir: Option<String>,
        #[arg(long)]
        start_args: Option<String>,
    },
    OpenProcess {
        #[arg(long)]
        injector_lib_path: String,
        #[arg(long)]
        start_method: String,
        #[arg(long)]
        exe_path: Option<String>,
        #[arg(long)]
        work_dir: Option<String>,
        #[arg(long, num_args = 1..)]
        start_args: Option<Vec<String>>,
        #[arg(long)]
        process_flags: Option<u32>,
        #[arg(long)]
        process_name: Option<String>,
        #[arg(long, num_args = 1.., requires = "process_name")]
        dll_paths: Option<Vec<String>>,
        #[arg(long)]
        cmd: Option<String>,
        #[arg(long, default_value_t = 15)]
        inject_timeout: i32,
    },
    HookAndWait {
        #[arg(long)]
        injector_lib_path: String,
        #[arg(long)]
        dll_path: String,
        #[arg(long)]
        target_process: String,
        #[arg(long, default_value_t = 15)]
        timeout: i32,
    },
    InjectLibraries {
        #[arg(long, num_args = 1..)]
        dll_paths: Vec<String>,
        #[arg(long, required_unless_present = "pid")]
        process_name: Option<String>,
        #[arg(long, required_unless_present = "process_name")]
        pid: Option<u32>,
        #[arg(long, default_value_t = 15)]
        timeout: i32,
    },
}

fn load_injector_lib(path: &str) -> Result<Library> {
    let path_obj = Path::new(path);
    if !path_obj.exists() {
        return Err(anyhow!("Injector file not found: {}", path));
    }
    let injector_path = path_obj.canonicalize()?;
    let lib =
        unsafe { libloading::os::windows::Library::load_with_flags(&injector_path, 0x00000008)? };
    Ok(lib.into())
}

fn main() -> Result<()> {
    let cli = Cli::parse();

    match cli.command {
        Commands::StartProcess {
            injector_lib_path,
            exe_path,
            work_dir,
            start_args,
        } => {
            let lib = load_injector_lib(&injector_lib_path)?;
            helper_start_process(
                &lib,
                &exe_path,
                work_dir.as_deref().unwrap_or_default(),
                start_args.as_deref().unwrap_or_default(),
            )?;
        }
        Commands::OpenProcess {
            injector_lib_path,
            start_method,
            exe_path,
            work_dir,
            start_args,
            process_flags,
            process_name,
            dll_paths,
            cmd,
            inject_timeout,
        } => {
            let lib = load_injector_lib(&injector_lib_path)?;
            let method = start_method.to_uppercase();

            if let Some(paths) = &dll_paths {
                for path in paths {
                    if !path.is_ascii() {
                        return Err(anyhow!(
              "Please rename all folders from the path using only English letters:\n{}",
              path
            ));
                    }
                }
            }

            match method.as_str() {
                "NATIVE" => {
                    let exe = exe_path
                        .as_ref()
                        .ok_or_else(|| anyhow!("exe_path required for NATIVE"))?;
                    let mut command = Command::new(exe);
                    if let Some(flags) = process_flags {
                        command.creation_flags(flags);
                    }
                    if let Some(wd) = &work_dir {
                        command.current_dir(wd);
                    }

                    if let Some(c) = &cmd {
                        let mut shell_cmd = Command::new("cmd.exe");
                        shell_cmd.args(["/C", c]);
                        if let Some(flags) = process_flags {
                            shell_cmd.creation_flags(flags);
                        }
                        if let Some(wd) = &work_dir {
                            shell_cmd.current_dir(wd);
                        }
                        shell_cmd.spawn()?;
                    } else {
                        if let Some(args) = &start_args {
                            command.args(args);
                        }
                        command.spawn()?;
                    }
                }
                "SHELL" => {
                    if let Some(c) = &cmd {
                        helper_start_process(&lib, "cmd.exe", "", &format!("/C \"{}\"", c))?;
                    } else {
                        let exe = exe_path
                            .as_ref()
                            .ok_or_else(|| anyhow!("exe_path required for SHELL"))?;
                        let args = start_args.as_ref().map(|a| a.join(" ")).unwrap_or_default();
                        helper_start_process(
                            &lib,
                            exe,
                            work_dir.as_deref().unwrap_or_default(),
                            &args,
                        )?;
                    }
                }
                "MANUAL" => {}
                _ => return Err(anyhow!("Unknown process start method `{}`!", method)),
            }

            if let Some(paths) = &dll_paths {
                helper_inject_libraries(paths, process_name.as_deref(), None, inject_timeout)?;
            }
        }
        Commands::HookAndWait {
            injector_lib_path,
            dll_path,
            target_process,
            timeout,
        } => {
            let lib = load_injector_lib(&injector_lib_path)?;
            let hook_library_fn: LibSymbol<HookLibraryFn> = unsafe {
                lib.get(b"HookLibrary")
                    .context("Failed to get HookLibrary")?
            };
            let wait_fn: LibSymbol<WaitForInjectionFn> = unsafe {
                lib.get(b"WaitForInjection")
                    .context("Failed to get WaitForInjection")?
            };
            let unhook_fn: LibSymbol<UnhookLibraryFn> = unsafe {
                lib.get(b"UnhookLibrary")
                    .context("Failed to get UnhookLibrary")?
            };

            let path_obj = Path::new(&dll_path);
            if !path_obj.exists() {
                return Err(anyhow!("DLL file not found: {}", dll_path));
            }

            let resolved_path = path_obj.canonicalize()?;
            let mut resolved_path_str = resolved_path.to_string_lossy().to_string();

            if resolved_path_str.starts_with(r"\\?\") {
                resolved_path_str = resolved_path_str[4..].to_string();
            }

            let w_dll_path = to_wstring(&resolved_path_str);
            let mut hook = HHOOK(0);
            let mut mutex = HANDLE(0);

            let result =
                unsafe { hook_library_fn(PCWSTR(w_dll_path.as_ptr()), &mut hook, &mut mutex) };

            match result {
                100 => return Err(anyhow!("Another instance of 3DMigotoLoader is running!")),
                200 => return Err(anyhow!("Failed to load {}!", dll_path)),
                300 => {
                    return Err(anyhow!(
                        "Library {} is missing expected entry point!",
                        dll_path
                    ))
                }
                400 => return Err(anyhow!("Failed to setup windows hook for {}!", dll_path)),
                0 => {}
                _ => return Err(anyhow!("Unknown error while hooking {}!", dll_path)),
            }

            if hook.0 == 0 {
                return Err(anyhow!("Hook is NULL for {}!", dll_path));
            }

            let w_target = to_wstring(&target_process);
            let wait_res = unsafe {
                wait_fn(
                    PCWSTR(w_dll_path.as_ptr()),
                    PCWSTR(w_target.as_ptr()),
                    timeout,
                )
            };
            let success = wait_res == 0;

            let _ = unsafe { unhook_fn(&mut hook, &mut mutex) };

            if !success {
                return Err(anyhow!("Wait for injection failed or timed out."));
            }
        }
        Commands::InjectLibraries {
            dll_paths,
            process_name,
            pid,
            timeout,
        } => {
            helper_inject_libraries(&dll_paths, process_name.as_deref(), pid, timeout)?;
        }
    }

    Ok(())
}
