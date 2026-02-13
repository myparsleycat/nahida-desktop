use std::ffi::OsStr;
use std::os::windows::ffi::OsStrExt;
use std::path::Path;
use std::process::Command;
use std::sync::Arc;
use std::thread;
use std::time::{Duration, Instant};

use libloading::{Error as LibLoadingError, Library, Symbol as LibSymbol};
use napi::bindgen_prelude::*;
use napi::{Env, Task};
use napi_derive::napi;
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
    lib
      .get(b"StartProcess")
      .map_err(|e: LibLoadingError| Error::from_reason(e.to_string()))?
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
    return Err(Error::from_reason(format!(
      "Failed to start {}: {} code {}",
      exe_path, error_text, result
    )));
  }

  Ok(())
}

fn helper_inject_libraries(
  dll_paths: &[String],
  process_name: Option<&str>,
  pid: Option<u32>,
  timeout: i32,
) -> Result<i32> {
  let t = timeout;
  let time_start = Instant::now();
  let mut sys = System::new();
  let refresh_kind = RefreshKind::new().with_processes(ProcessRefreshKind::new());

  loop {
    if t != -1 && time_start.elapsed().as_secs_f64() >= t as f64 {
      return Ok(-1);
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
          if let Err(e) = native_inject(current_pid_u32, dll_path) {
            return Err(Error::from_reason(format!(
                            "Failed to inject extra library {}:\n{}!\nPlease check Advanced Settings -> Inject Libraries.",
                            dll_path, e
                        )));
          }
        }
        return Ok(current_pid_u32 as i32);
      }
    }

    thread::sleep(Duration::from_millis(100));
  }
}

pub struct OpenProcessTask {
  lib: Arc<Library>,
  start_method: String,
  exe_path: Option<String>,
  work_dir: Option<String>,
  start_args: Option<Vec<String>>,
  process_flags: Option<u32>,
  process_name: Option<String>,
  dll_paths: Option<Vec<String>>,
  cmd: Option<String>,
  inject_timeout: Option<i32>,
}

impl Task for OpenProcessTask {
  type Output = ();
  type JsValue = ();

  fn compute(&mut self) -> Result<Self::Output> {
    let method = self.start_method.to_uppercase();
    let timeout = self.inject_timeout.unwrap_or(15);

    if let Some(paths) = &self.dll_paths {
      for path in paths {
        if !path.is_ascii() {
          return Err(Error::from_reason(format!(
            "Please rename all folders from the path using only English letters:\n{}",
            path
          )));
        }
      }
    }

    match method.as_str() {
      "NATIVE" => {
        let exe = self
          .exe_path
          .as_ref()
          .ok_or_else(|| Error::from_reason("exe_path required for NATIVE"))?;
        let mut command = Command::new(exe);

        if let Some(flags) = self.process_flags {
          command.creation_flags(flags);
        }

        if let Some(wd) = &self.work_dir {
          command.current_dir(wd);
        }

        if let Some(c) = &self.cmd {
          let mut shell_cmd = Command::new("cmd.exe");
          shell_cmd.args(["/C", c]);
          if let Some(flags) = self.process_flags {
            shell_cmd.creation_flags(flags);
          }
          if let Some(wd) = &self.work_dir {
            shell_cmd.current_dir(wd);
          }
          shell_cmd
            .spawn()
            .map_err(|e| Error::from_reason(e.to_string()))?;
        } else {
          if let Some(args) = &self.start_args {
            command.args(args);
          }
          command
            .spawn()
            .map_err(|e| Error::from_reason(e.to_string()))?;
        }
      }
      "SHELL" => {
        if let Some(c) = &self.cmd {
          helper_start_process(&self.lib, "cmd.exe", "", &format!("/C \"{}\"", c))?;
        } else {
          let exe = self
            .exe_path
            .as_ref()
            .ok_or_else(|| Error::from_reason("exe_path required for SHELL"))?;
          let args = self
            .start_args
            .as_ref()
            .map(|a| a.join(" "))
            .unwrap_or_default();
          helper_start_process(
            &self.lib,
            exe,
            self.work_dir.as_deref().unwrap_or_default(),
            &args,
          )?;
        }
      }
      "MANUAL" => {}
      _ => {
        return Err(Error::from_reason(format!(
          "Unknown process start method `{}`!",
          method
        )))
      }
    }

    if let Some(paths) = &self.dll_paths {
      let pid = helper_inject_libraries(paths, self.process_name.as_deref(), None, timeout)?;
      if pid == -1 {
        return Err(Error::from_reason("Failed to inject libraries!"));
      }
    }

    Ok(())
  }

  fn resolve(&mut self, _env: Env, _output: Self::Output) -> Result<Self::JsValue> {
    Ok(())
  }
}

pub struct WaitForInjectionTask {
  lib: Arc<Library>,
  w_dll_path: Vec<u16>,
  target_process: String,
  timeout: i32,
}

impl Task for WaitForInjectionTask {
  type Output = bool;
  type JsValue = bool;

  fn compute(&mut self) -> Result<Self::Output> {
    let wait_fn: LibSymbol<WaitForInjectionFn> = unsafe {
      self
        .lib
        .get(b"WaitForInjection")
        .map_err(|e: LibLoadingError| Error::from_reason(e.to_string()))?
    };

    let w_dll_ptr = self.w_dll_path.as_ptr();
    let w_target = to_wstring(&self.target_process);

    let result = unsafe { wait_fn(PCWSTR(w_dll_ptr), PCWSTR(w_target.as_ptr()), self.timeout) };

    Ok(result == 0)
  }

  fn resolve(&mut self, _env: Env, output: Self::Output) -> Result<Self::JsValue> {
    Ok(output)
  }
}

pub struct InjectLibrariesTask {
  dll_paths: Vec<String>,
  process_name: Option<String>,
  pid: Option<u32>,
  timeout: i32,
}

impl Task for InjectLibrariesTask {
  type Output = i32;
  type JsValue = i32;

  fn compute(&mut self) -> Result<Self::Output> {
    helper_inject_libraries(
      &self.dll_paths,
      self.process_name.as_deref(),
      self.pid,
      self.timeout,
    )
  }

  fn resolve(&mut self, _env: Env, output: Self::Output) -> Result<Self::JsValue> {
    Ok(output)
  }
}

#[napi]
pub struct DllInjector {
  lib: Option<Arc<Library>>,
  dll_path: Option<String>,
  w_dll_path: Option<Vec<u16>>,
  target_process: Option<String>,
  hook: HHOOK,
  mutex: HANDLE,
}

#[napi]
impl DllInjector {
  #[napi(constructor)]
  pub fn new(injector_lib_path: String) -> Result<Self> {
    let path_obj = Path::new(&injector_lib_path);
    if !path_obj.exists() {
      return Err(Error::from_reason(format!(
        "Injector file not found: {}!",
        injector_lib_path
      )));
    }

    let injector_path = path_obj
      .canonicalize()
      .map_err(|e| Error::from_reason(format!("Failed to resolve injector path: {}", e)))?;
    let injector_dir = injector_path
      .parent()
      .ok_or_else(|| Error::from_reason("Failed to get injector parent directory"))?;

    let original_dir = std::env::current_dir()
      .map_err(|e| Error::from_reason(format!("Failed to get current directory: {}", e)))?;

    std::env::set_current_dir(injector_dir)
      .map_err(|e| Error::from_reason(format!("Failed to set CWD to injector dir: {}", e)))?;

    let lib_result = unsafe { Library::new(&injector_path) };

    let _ = std::env::set_current_dir(original_dir);

    let lib = lib_result.map_err(|e: LibLoadingError| {
      Error::from_reason(format!("Failed to load injector library! {}", e))
    })?;

    unsafe {
      let _hook_lib: LibSymbol<HookLibraryFn> =
        lib.get(b"HookLibrary").map_err(|e: LibLoadingError| {
          Error::from_reason(format!("Failed to setup injector library! {}", e))
        })?;
      let _wait_inj: LibSymbol<WaitForInjectionFn> =
        lib.get(b"WaitForInjection").map_err(|e: LibLoadingError| {
          Error::from_reason(format!("Failed to setup injector library! {}", e))
        })?;
      let _unhook_lib: LibSymbol<UnhookLibraryFn> =
        lib.get(b"UnhookLibrary").map_err(|e: LibLoadingError| {
          Error::from_reason(format!("Failed to setup injector library! {}", e))
        })?;
    }

    Ok(DllInjector {
      lib: Some(Arc::new(lib)),
      dll_path: None,
      w_dll_path: None,
      target_process: None,
      hook: HHOOK(0),
      mutex: HANDLE(0),
    })
  }

  #[napi]
  pub fn unload(&mut self) -> Result<()> {
    if let Some(lib) = self.lib.take() {
      drop(lib);
      Ok(())
    } else {
      Err(Error::from_reason("Failed to unload injector library!"))
    }
  }

  #[napi]
  pub fn start_process(
    &self,
    exe_path: String,
    work_dir: Option<String>,
    start_args: Option<String>,
  ) -> Result<()> {
    let lib = self
      .lib
      .as_ref()
      .ok_or_else(|| Error::from_reason("Library not loaded"))?;
    helper_start_process(
      lib,
      &exe_path,
      work_dir.as_deref().unwrap_or_default(),
      start_args.as_deref().unwrap_or_default(),
    )
  }

  #[napi]
  pub fn open_process(
    &self,
    start_method: String,
    exe_path: Option<String>,
    work_dir: Option<String>,
    start_args: Option<Vec<String>>,
    process_flags: Option<u32>,
    process_name: Option<String>,
    dll_paths: Option<Vec<String>>,
    cmd: Option<String>,
    inject_timeout: Option<i32>,
  ) -> Result<AsyncTask<OpenProcessTask>> {
    let lib = self
      .lib
      .as_ref()
      .ok_or_else(|| Error::from_reason("Library not loaded"))?
      .clone();

    Ok(AsyncTask::new(OpenProcessTask {
      lib,
      start_method,
      exe_path,
      work_dir,
      start_args,
      process_flags,
      process_name,
      dll_paths,
      cmd,
      inject_timeout,
    }))
  }

  #[napi]
  pub fn hook_library(&mut self, dll_path: String, target_process: String) -> Result<()> {
    if self.hook.0 != 0 {
      let _ = self.unhook_library();
      return Err(Error::from_reason(format!(
        "Invalid injector usage: {} was not unhooked!",
        dll_path
      )));
    }

    let lib = self
      .lib
      .as_ref()
      .ok_or_else(|| Error::from_reason("Library not loaded"))?;

    let hook_library_fn: LibSymbol<HookLibraryFn> = unsafe {
      lib
        .get(b"HookLibrary")
        .map_err(|e: LibLoadingError| Error::from_reason(e.to_string()))?
    };

    let path_obj = Path::new(&dll_path);

    if !path_obj.exists() {
      return Err(Error::from_reason(format!(
        "DLL file not found: {}",
        dll_path
      )));
    }

    let resolved_path = path_obj
      .canonicalize()
      .map_err(|e| Error::from_reason(format!("Failed to resolve DLL path {}: {}", dll_path, e)))?;

    let resolved_path_str = resolved_path.to_string_lossy().to_string();

    let resolved_path_str = if resolved_path_str.starts_with(r"\\?\") {
      resolved_path_str[4..].to_string()
    } else {
      resolved_path_str
    };

    let w_dll_path = to_wstring(&resolved_path_str);
    self.w_dll_path = Some(w_dll_path.clone());
    let w_dll_path_ptr = self.w_dll_path.as_ref().unwrap().as_ptr();

    let dll_dir = resolved_path
      .parent()
      .ok_or_else(|| Error::from_reason("Failed to get DLL parent directory"))?;

    let original_dir = std::env::current_dir()
      .map_err(|e| Error::from_reason(format!("Failed to get current directory: {}", e)))?;

    std::env::set_current_dir(dll_dir)
      .map_err(|e| Error::from_reason(format!("Failed to set current directory: {}", e)))?;

    let result =
      unsafe { hook_library_fn(PCWSTR(w_dll_path_ptr), &mut self.hook, &mut self.mutex) };

    let _ = std::env::set_current_dir(original_dir);

    match result {
      100 => {
        return Err(Error::from_reason(
          "Another instance of 3DMigotoLoader is running!",
        ))
      }
      200 => return Err(Error::from_reason(format!("Failed to load {}!", dll_path))),
      300 => {
        return Err(Error::from_reason(format!(
          "Library {} is missing expected entry point!",
          dll_path
        )))
      }
      400 => {
        return Err(Error::from_reason(format!(
          "Failed to setup windows hook for {}!",
          dll_path
        )))
      }
      0 => {}
      _ => {
        return Err(Error::from_reason(format!(
          "Unknown error while hooking {}!",
          dll_path
        )))
      }
    }

    if self.hook.0 == 0 {
      return Err(Error::from_reason(format!(
        "Hook is NULL for {}!",
        dll_path
      )));
    }

    self.dll_path = Some(resolved_path_str);
    self.target_process = Some(target_process);

    Ok(())
  }

  #[napi]
  pub fn wait_for_injection(
    &self,
    timeout: Option<i32>,
  ) -> Result<AsyncTask<WaitForInjectionTask>> {
    let t = timeout.unwrap_or(15);
    if self.dll_path.is_none() {
      return Err(Error::from_reason(
        "Invalid injector usage: dll path is not defined!",
      ));
    }
    if self.w_dll_path.is_none() {
      return Err(Error::from_reason(
        "Invalid injector usage: dll path (wide) is not defined!",
      ));
    }
    if self.target_process.is_none() {
      return Err(Error::from_reason(
        "Invalid injector usage: target process is not defined!",
      ));
    }
    if self.hook.0 == 0 {
      return Err(Error::from_reason(
        "Invalid injector usage: dll is not hooked!",
      ));
    }

    let lib = self
      .lib
      .as_ref()
      .ok_or_else(|| Error::from_reason("Library not loaded"))?
      .clone();

    let w_dll_path = self.w_dll_path.as_ref().unwrap().clone();
    let target_process = self.target_process.as_ref().unwrap().clone();

    Ok(AsyncTask::new(WaitForInjectionTask {
      lib,
      w_dll_path,
      target_process,
      timeout: t,
    }))
  }

  #[napi]
  pub fn unhook_library(&mut self) -> Result<bool> {
    if self.hook.0 == 0 && self.mutex.0 == 0 {
      return Ok(true);
    }

    let lib = self
      .lib
      .as_ref()
      .ok_or_else(|| Error::from_reason("Library not loaded"))?;

    let unhook_fn: LibSymbol<UnhookLibraryFn> = unsafe {
      lib
        .get(b"UnhookLibrary")
        .map_err(|e: LibLoadingError| Error::from_reason(e.to_string()))?
    };

    let result = unsafe { unhook_fn(&mut self.hook, &mut self.mutex) };

    self.dll_path = None;
    self.w_dll_path = None;
    self.target_process = None;
    self.hook = HHOOK(0);
    self.mutex = HANDLE(0);

    Ok(result == 0)
  }

  #[napi]
  pub fn inject_libraries(
    &self,
    dll_paths: Vec<String>,
    process_name: Option<String>,
    pid: Option<u32>,
    timeout: Option<i32>,
  ) -> Result<AsyncTask<InjectLibrariesTask>> {
    let t = timeout.unwrap_or(15);
    Ok(AsyncTask::new(InjectLibrariesTask {
      dll_paths,
      process_name,
      pid,
      timeout: t,
    }))
  }
}

use injector::Injector;

fn native_inject(pid: u32, dll_path: &str) -> std::result::Result<(), String> {
  let injector = Injector::attach(pid).map_err(|e| format!("Failed to attach injector: {}", e))?;
  injector
    .inject(dll_path)
    .map_err(|e| format!("Failed to inject DLL: {}", e))?;
  Ok(())
}
