use ddsfile::Dds;
use image::ImageFormat;
use jwalk::WalkDir;
use napi::bindgen_prelude::AsyncTask;
use napi::Task;
use napi_derive::napi;
use rayon::prelude::*;
use std::collections::HashSet;
use std::fs::File;
use std::io::BufReader;
use std::path::PathBuf;
use std::sync::{
    atomic::{AtomicBool, Ordering},
    Arc, Mutex,
};
use std::thread;
use std::time::{Duration, Instant};
use sysinfo::Disks;
use windows::core::{PCWSTR, PWSTR};
use windows::Win32::Foundation::{CloseHandle, BOOL, HANDLE, HWND, LPARAM};
use windows::Win32::System::Diagnostics::ToolHelp::{
    CreateToolhelp32Snapshot, Process32First, Process32Next, PROCESSENTRY32, TH32CS_SNAPPROCESS,
};
use windows::Win32::System::Threading::{
    CreateProcessW, GetExitCodeProcess, OpenProcess, QueryFullProcessImageNameW, TerminateProcess,
    WaitForSingleObject, CREATE_NEW_PROCESS_GROUP, INFINITE, PROCESS_INFORMATION,
    PROCESS_NAME_WIN32, PROCESS_QUERY_LIMITED_INFORMATION, PROCESS_TERMINATE, STARTUPINFOW,
};
use windows::Win32::UI::Shell::{ShellExecuteExW, SEE_MASK_NOCLOSEPROCESS, SHELLEXECUTEINFOW};
use windows::Win32::UI::WindowsAndMessaging::GetForegroundWindow;
use windows::Win32::UI::WindowsAndMessaging::SW_HIDE;
use windows::Win32::UI::WindowsAndMessaging::{
    EnumWindows, GetWindowThreadProcessId, IsIconic, IsWindowVisible,
};

fn to_wstring(str: &str) -> Vec<u16> {
    use std::ffi::OsStr;
    use std::os::windows::ffi::OsStrExt;
    OsStr::new(str).encode_wide().chain(Some(0)).collect()
}

struct HandleWrapper(HANDLE);

impl Drop for HandleWrapper {
    fn drop(&mut self) {
        unsafe {
            let _ = CloseHandle(self.0);
        }
    }
}

pub struct ConvertDdsToPngTask {
    input_path: PathBuf,
    output_path: PathBuf,
}

#[napi]
impl Task for ConvertDdsToPngTask {
    type Output = ();
    type JsValue = ();

    fn compute(&mut self) -> napi::Result<Self::Output> {
        let file = File::open(&self.input_path).map_err(|error| {
            napi::Error::from_reason(format!(
                "Failed to open DDS file '{}': {}",
                self.input_path.display(),
                error
            ))
        })?;

        let mut reader = BufReader::new(file);
        let dds = Dds::read(&mut reader).map_err(|error| {
            napi::Error::from_reason(format!(
                "Failed to read DDS file '{}': {}",
                self.input_path.display(),
                error
            ))
        })?;

        let image = image_dds::image_from_dds(&dds, 0).map_err(|error| {
            napi::Error::from_reason(format!(
                "Failed to decode DDS file '{}': {}",
                self.input_path.display(),
                error
            ))
        })?;

        if let Some(parent) = self.output_path.parent() {
            std::fs::create_dir_all(parent).map_err(|error| {
                napi::Error::from_reason(format!(
                    "Failed to create output directory '{}': {}",
                    parent.display(),
                    error
                ))
            })?;
        }

        image
            .save_with_format(&self.output_path, ImageFormat::Png)
            .map_err(|error| {
                napi::Error::from_reason(format!(
                    "Failed to save PNG file '{}': {}",
                    self.output_path.display(),
                    error
                ))
            })?;

        Ok(())
    }

    fn resolve(&mut self, _: napi::Env, _: Self::Output) -> napi::Result<Self::JsValue> {
        Ok(())
    }
}

#[napi]
pub fn convert_dds_to_png(
    input_path: String,
    output_path: String,
) -> AsyncTask<ConvertDdsToPngTask> {
    AsyncTask::new(ConvertDdsToPngTask {
        input_path: PathBuf::from(input_path),
        output_path: PathBuf::from(output_path),
    })
}

#[napi]
pub enum ProcessWindowState {
    Open,
    Minimized,
    NotFound,
}

struct EnumState {
    target_pid: u32,
    result: ProcessWindowState,
}

struct ZOrderState {
    target_pids: HashSet<u32>,
    found_pid: Option<u32>,
}

#[napi]
pub fn get_process_window_state(pid: u32) -> ProcessWindowState {
    let mut state = EnumState {
        target_pid: pid,
        result: ProcessWindowState::NotFound,
    };

    unsafe {
        let _ = EnumWindows(
            Some(enum_window_callback),
            LPARAM(&mut state as *mut EnumState as isize),
        );
    }

    state.result
}

#[napi]
pub fn get_topmost_pid(pids: Vec<u32>) -> Option<u32> {
    if pids.is_empty() {
        return None;
    }

    let mut state = ZOrderState {
        target_pids: pids.into_iter().collect(),
        found_pid: None,
    };

    unsafe {
        let _ = EnumWindows(
            Some(enum_z_order_callback),
            LPARAM(&mut state as *mut ZOrderState as isize),
        );
    }

    state.found_pid
}

unsafe extern "system" fn enum_window_callback(hwnd: HWND, lparam: LPARAM) -> BOOL {
    let state = &mut *(lparam.0 as *mut EnumState);
    let mut window_pid = 0;

    GetWindowThreadProcessId(hwnd, Some(&mut window_pid));

    if window_pid == state.target_pid {
        if IsWindowVisible(hwnd).as_bool() {
            if IsIconic(hwnd).as_bool() {
                state.result = ProcessWindowState::Minimized;
            } else {
                state.result = ProcessWindowState::Open;
            }
            return BOOL(0);
        }
    }

    BOOL(1)
}

unsafe extern "system" fn enum_z_order_callback(hwnd: HWND, lparam: LPARAM) -> BOOL {
    let state = &mut *(lparam.0 as *mut ZOrderState);

    let mut window_pid = 0;
    GetWindowThreadProcessId(hwnd, Some(&mut window_pid));

    if state.target_pids.contains(&window_pid) {
        if IsWindowVisible(hwnd).as_bool() {
            state.found_pid = Some(window_pid);
            return BOOL(0);
        }
    }
    BOOL(1)
}

struct FocusTracker {
    pub history: Vec<u32>,
}

impl FocusTracker {
    fn new() -> Self {
        Self {
            history: Vec::new(),
        }
    }

    fn push(&mut self, pid: u32) {
        if self.history.last() != Some(&pid) {
            self.history.push(pid);
            if self.history.len() > 30 {
                self.history.remove(0);
            }
        }
    }
}

lazy_static::lazy_static! {
    static ref TRACKER: Arc<Mutex<FocusTracker>> = Arc::new(Mutex::new(FocusTracker::new()));
    static ref TRACKING_STARTED: AtomicBool = AtomicBool::new(false);
}

#[napi]
pub fn start_tracking() {
    if TRACKING_STARTED
        .compare_exchange(false, true, Ordering::SeqCst, Ordering::SeqCst)
        .is_err()
    {
        return;
    }

    let tracker = TRACKER.clone();
    thread::spawn(move || loop {
        unsafe {
            let hwnd = GetForegroundWindow();
            if hwnd.0 != 0 {
                let mut pid = 0;
                GetWindowThreadProcessId(hwnd, Some(&mut pid));
                if pid != 0 {
                    let mut tracker = tracker.lock().unwrap();
                    tracker.push(pid);
                }
            }
        }
        thread::sleep(Duration::from_millis(100));
    });
}

#[napi]
pub fn get_previous_pids(current_pid: u32) -> Vec<u32> {
    let tracker = TRACKER.lock().unwrap();
    let mut pids = Vec::new();

    for &pid in tracker.history.iter().rev() {
        if pid != current_pid && !pids.contains(&pid) {
            pids.push(pid);
            if pids.len() >= 5 {
                break;
            }
        }
    }
    pids
}

#[napi]
pub fn get_process_name(pid: u32) -> Option<String> {
    unsafe {
        let handle = match OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, false, pid) {
            Ok(h) => HandleWrapper(h),
            Err(_) => return None,
        };

        let mut buffer = [0u16; 1024];
        let mut size = buffer.len() as u32;

        if QueryFullProcessImageNameW(
            handle.0,
            PROCESS_NAME_WIN32,
            PWSTR(buffer.as_mut_ptr()),
            &mut size,
        )
        .is_ok()
        {
            use std::ffi::OsString;
            use std::os::windows::ffi::OsStringExt;
            let path = OsString::from_wide(&buffer[..size as usize]);
            if let Ok(full_path) = path.into_string() {
                if let Some(filename) = std::path::Path::new(&full_path).file_name() {
                    return filename.to_str().map(|s| s.to_string());
                }
            }
        }
    }
    None
}

#[napi(object)]
pub struct SearchOptions {
    pub exclude_dirs: Option<Vec<String>>,
}

#[napi]
pub async fn find_file_across_drives(
    target_file_name: String,
    options: Option<SearchOptions>,
) -> Option<String> {
    let disks = Disks::new_with_refreshed_list();
    let base_exclude_paths = vec!["C:\\Windows"];

    let stop_signal = Arc::new(AtomicBool::new(false));

    let exclude_dirs: HashSet<String> = options
        .and_then(|o| o.exclude_dirs)
        .unwrap_or_default()
        .into_iter()
        .map(|s| {
            s.trim_start_matches('/')
                .trim_start_matches('\\')
                .to_string()
        })
        .collect();

    let exclude_dirs_arc = Arc::new(exclude_dirs);

    let drives: Vec<_> = disks
        .iter()
        .map(|disk| disk.mount_point().to_path_buf())
        .filter(|root| {
            let root_str = root.to_string_lossy();
            !base_exclude_paths.iter().any(|ex| root_str.starts_with(ex))
        })
        .collect();

    drives.into_par_iter().find_map_any(|root_path| {
        let exclude_dirs = exclude_dirs_arc.clone();
        for entry in WalkDir::new(root_path)
            .skip_hidden(false)
            .follow_links(false)
            .process_read_dir(move |_depth, _path, _state, children| {
                children.retain(|child| {
                    if let Ok(child) = child {
                        if child.file_type().is_dir() {
                            let name = child.file_name().to_string_lossy();
                            return !exclude_dirs.contains(name.as_ref());
                        }
                    }
                    true
                });
            })
        {
            if stop_signal.load(Ordering::Relaxed) {
                return None;
            }

            if let Ok(entry) = entry {
                if entry.file_name().to_string_lossy() == target_file_name {
                    stop_signal.store(true, Ordering::Relaxed);
                    return Some(entry.path().to_string_lossy().to_string());
                }
            }
        }
        None
    })
}

pub const WAIT_RESULT_FOUND: i32 = 0;
pub const WAIT_RESULT_NOT_FOUND: i32 = -100;
pub const WAIT_RESULT_TIMEOUT: i32 = -200;
pub const WAIT_RESULT_TERMINATED: i32 = -300;

#[napi(object)]
pub struct WaitResultObject {
    pub found: i32,
    pub not_found: i32,
    pub timeout: i32,
    pub terminated: i32,
}

#[napi]
pub fn get_wait_result() -> WaitResultObject {
    WaitResultObject {
        found: WAIT_RESULT_FOUND,
        not_found: WAIT_RESULT_NOT_FOUND,
        timeout: WAIT_RESULT_TIMEOUT,
        terminated: WAIT_RESULT_TERMINATED,
    }
}

#[napi(object)]
pub struct ProcessInfo {
    pub pid: u32,
    pub name: String,
}

#[napi(object)]
pub struct WaitResponse {
    pub result: i32,
    pub pid: u32,
}

fn parse_exe_name(chars: &[u8; 260]) -> String {
    let null_pos = chars.iter().position(|&c| c == 0).unwrap_or(260);
    String::from_utf8_lossy(&chars[..null_pos]).to_string()
}

struct CallbackState {
    target_pid: u32,
    check_visibility: bool,
    hwnds: Vec<i64>,
}

#[napi]
pub fn get_hwnds_for_pid(pid: u32, check_visibility: bool) -> Vec<i64> {
    let state = Box::new(Mutex::new(CallbackState {
        target_pid: pid,
        check_visibility,
        hwnds: Vec::new(),
    }));

    let state_ptr = Box::into_raw(state);

    unsafe {
        let _ = EnumWindows(Some(get_hwnds_callback), LPARAM(state_ptr as isize));

        let state_box = Box::from_raw(state_ptr);
        let state_guard = state_box.lock().unwrap();
        state_guard.hwnds.clone()
    }
}

unsafe extern "system" fn get_hwnds_callback(hwnd: HWND, lparam: LPARAM) -> BOOL {
    let state_ptr = lparam.0 as *mut Mutex<CallbackState>;
    let state = &*state_ptr;

    if let Ok(mut state_guard) = state.lock() {
        let mut window_pid = 0;
        GetWindowThreadProcessId(hwnd, Some(&mut window_pid));

        if window_pid == state_guard.target_pid {
            if state_guard.check_visibility {
                let is_visible = IsWindowVisible(hwnd).as_bool();
                let is_iconic = IsIconic(hwnd).as_bool();
                if !is_visible || is_iconic {
                    return BOOL(1);
                }
            }
            state_guard.hwnds.push(hwnd.0 as i64);
        }
    }

    BOOL(1)
}

#[napi]
pub fn get_process(process_id: Option<u32>, process_name: Option<String>) -> Option<ProcessInfo> {
    unsafe {
        let snapshot = match CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS, 0) {
            Ok(h) => HandleWrapper(h),
            Err(_) => return None,
        };

        let mut pe32 = PROCESSENTRY32 {
            dwSize: std::mem::size_of::<PROCESSENTRY32>() as u32,
            ..Default::default()
        };

        if Process32First(snapshot.0, &mut pe32).is_err() {
            return None;
        }

        loop {
            let current_name = parse_exe_name(&pe32.szExeFile);
            let current_pid = pe32.th32ProcessID;

            let mut is_match = false;

            if let Some(ref name) = process_name {
                if current_name.eq_ignore_ascii_case(name) {
                    is_match = true;
                }
            }

            if let Some(pid) = process_id {
                if current_pid == pid {
                    is_match = true;
                }
            }

            if is_match {
                return Some(ProcessInfo {
                    pid: current_pid,
                    name: current_name,
                });
            }

            if Process32Next(snapshot.0, &mut pe32).is_err() {
                break;
            }
        }
        None
    }
}

#[napi]
pub fn kill_process(pid: u32) -> bool {
    unsafe {
        if let Ok(handle) = OpenProcess(PROCESS_TERMINATE, false, pid) {
            let wrapper = HandleWrapper(handle);
            let result = TerminateProcess(wrapper.0, 1);
            return result.is_ok();
        }
        false
    }
}

#[napi(object)]
pub struct WaitForProcessOptions {
    pub process_name: String,
    pub timeout: Option<f64>,
    pub with_window: Option<bool>,
    pub check_visibility: Option<bool>,
}

#[napi]
pub async fn wait_for_process(options: WaitForProcessOptions) -> WaitResponse {
    let process_name = options.process_name;
    let timeout = options.timeout.unwrap_or(10.0);
    let with_window = options.with_window.unwrap_or(false);
    let check_visibility = options.check_visibility.unwrap_or(false);

    let start_time = Instant::now();

    loop {
        let current_time = Instant::now();
        let elapsed = current_time.duration_since(start_time).as_secs_f64();

        if timeout > 0.0 && elapsed >= timeout {
            return WaitResponse {
                result: WAIT_RESULT_TIMEOUT,
                pid: 0,
            };
        }

        if let Some(process) = get_process(None, Some(process_name.clone())) {
            let pid = process.pid;

            if !with_window {
                return WaitResponse {
                    result: WAIT_RESULT_FOUND,
                    pid,
                };
            } else {
                let hwnds = get_hwnds_for_pid(pid, check_visibility);
                if !hwnds.is_empty() {
                    return WaitResponse {
                        result: WAIT_RESULT_FOUND,
                        pid,
                    };
                }
            }
        }

        thread::sleep(Duration::from_millis(100));
    }
}

#[napi(object)]
pub struct WaitForProcessExitOptions {
    pub process_name: String,
    pub timeout: Option<f64>,
    pub kill_timeout: Option<f64>,
}

#[napi]
pub async fn wait_for_process_exit(options: WaitForProcessExitOptions) -> WaitResponse {
    let process_name = options.process_name;
    let timeout = options.timeout.unwrap_or(10.0);
    let kill_timeout = options.kill_timeout.unwrap_or(-1.0);

    let start_time = Instant::now();

    loop {
        let current_time = Instant::now();
        let elapsed = current_time.duration_since(start_time).as_secs_f64();

        if timeout > 0.0 && elapsed >= timeout {
            return WaitResponse {
                result: WAIT_RESULT_TIMEOUT,
                pid: 0,
            };
        }

        if let Some(process) = get_process(None, Some(process_name.clone())) {
            let pid = process.pid;

            if kill_timeout > 0.0 && elapsed >= kill_timeout {
                if kill_process(pid) {
                    return WaitResponse {
                        result: WAIT_RESULT_TERMINATED,
                        pid,
                    };
                }
            }
        } else {
            return WaitResponse {
                result: WAIT_RESULT_NOT_FOUND,
                pid: 0,
            };
        }

        thread::sleep(Duration::from_millis(100));
    }
}

#[napi(object)]
pub struct SpawnOptions {
    pub exe_path: String,
    pub args: Option<String>,
    pub working_dir: Option<String>,
}

#[napi]
pub fn spawn_process(options: SpawnOptions) -> napi::Result<u32> {
    use std::ffi::OsStr;
    use std::os::windows::ffi::OsStrExt;

    let mut command_line: Vec<u16>;

    if let Some(args) = options.args {
        let full_cmd = format!("\"{}\" {}", options.exe_path, args);
        command_line = OsStr::new(&full_cmd)
            .encode_wide()
            .chain(std::iter::once(0))
            .collect();
    } else {
        command_line = OsStr::new(&options.exe_path)
            .encode_wide()
            .chain(std::iter::once(0))
            .collect();
    }

    let current_dir = options.working_dir.map(|dir| {
        OsStr::new(&dir)
            .encode_wide()
            .chain(std::iter::once(0))
            .collect::<Vec<u16>>()
    });

    unsafe {
        let mut si = STARTUPINFOW::default();
        si.cb = std::mem::size_of::<STARTUPINFOW>() as u32;

        let mut pi = PROCESS_INFORMATION::default();

        let result = CreateProcessW(
            PCWSTR::null(),
            PWSTR(command_line.as_mut_ptr()),
            None,
            None,
            false,
            CREATE_NEW_PROCESS_GROUP,
            None,
            current_dir
                .as_ref()
                .map(|d| PCWSTR(d.as_ptr()))
                .unwrap_or(PCWSTR::null()),
            &si,
            &mut pi,
        );

        if result.is_err() {
            return Err(napi::Error::from_reason(format!(
                "Failed to spawn process: {:?}",
                result.err()
            )));
        }

        let pid = pi.dwProcessId;

        let _ = CloseHandle(pi.hProcess);
        let _ = CloseHandle(pi.hThread);

        Ok(pid)
    }
}

#[napi]
pub async fn spawn_privileged_process(
    exe_path: String,
    args: String,
    working_dir: Option<String>,
) -> napi::Result<i32> {
    let w_exe_path = to_wstring(&exe_path);
    let w_args = to_wstring(&args);
    let w_working_dir = working_dir.map(|d| to_wstring(&d));
    let w_verb = to_wstring("runas");

    let mut info = SHELLEXECUTEINFOW::default();
    info.cbSize = std::mem::size_of::<SHELLEXECUTEINFOW>() as u32;
    info.fMask = SEE_MASK_NOCLOSEPROCESS;
    info.lpVerb = PCWSTR(w_verb.as_ptr());
    info.lpFile = PCWSTR(w_exe_path.as_ptr());
    info.lpParameters = PCWSTR(w_args.as_ptr());
    info.lpDirectory = w_working_dir
        .as_ref()
        .map(|d| PCWSTR(d.as_ptr()))
        .unwrap_or(PCWSTR::null());
    info.nShow = SW_HIDE.0 as i32;

    unsafe {
        let result = ShellExecuteExW(&mut info);
        if result.is_err() {
            return Err(napi::Error::from_reason(format!(
                "ShellExecuteExW failed: {:?}",
                result.err()
            )));
        }

        let h_process = info.hProcess;
        if h_process.is_invalid() {
            return Err(napi::Error::from_reason("Failed to get process handle"));
        }

        let exit_code = thread::spawn(move || {
            let _ = WaitForSingleObject(h_process, INFINITE);
            let mut code = 0;
            let _ = GetExitCodeProcess(h_process, &mut code);
            let _ = CloseHandle(h_process);
            code
        });

        let code = exit_code.join().map_err(|_| {
            napi::Error::from_reason("Internal thread panic while waiting for process")
        })?;

        Ok(code as i32)
    }
}
